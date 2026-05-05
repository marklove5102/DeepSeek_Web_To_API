package requestguard

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"DeepSeek_Web_To_API/internal/chathistory"
	"DeepSeek_Web_To_API/internal/config"
	"DeepSeek_Web_To_API/internal/requestmeta"
	"DeepSeek_Web_To_API/internal/responsecache"
	"DeepSeek_Web_To_API/internal/safetystore"
)

const (
	defaultBlockMessage = "request blocked by safety policy"
	maxScanBodyBytes    = 64 << 20
	maxCollectedText    = 512 << 10
)

type contextKey string

const metadataContextKey contextKey = "request_guard_metadata"

type Options struct {
	Store       *config.Store
	ChatHistory *chathistory.Store
	// SafetyWords / SafetyIPs are the dedicated SQLite stores for the
	// safety-policy lists. When wired they are the source of truth; the
	// legacy config.SafetyConfig list fields still take effect as a
	// fallback for setups that have not yet migrated.
	SafetyWords *safetystore.WordsStore
	SafetyIPs   *safetystore.IPsStore
}

type decision struct {
	blocked bool
	code    string
	detail  string
}

type policy struct {
	enabled                bool
	blockMessage           string
	blockedIPs             []ipMatcher
	blockedConversationIDs map[string]struct{}
	bannedContent          []string
	bannedRegex            []*regexp.Regexp
	jailbreakEnabled       bool
	jailbreakPatterns      []string
}

type ipMatcher struct {
	raw  string
	ip   net.IP
	cidr *net.IPNet
}

type policyCache struct {
	store       *config.Store
	safetyWords *safetystore.WordsStore
	safetyIPs   *safetystore.IPsStore
	mu          sync.Mutex
	signature   string
	cached      policy
}

var defaultJailbreakPatterns = []string{
	"ignore previous instructions",
	"disregard previous instructions",
	"ignore all previous instructions",
	"bypass safety",
	"disable safety",
	"developer mode",
	"dan mode",
	"jailbreak",
	"system prompt leak",
	"reveal your system prompt",
	"忽略之前",
	"无视之前",
	"无视系统",
	"绕过安全",
	"关闭安全",
	"泄露系统提示",
}

func Middleware(opts Options) func(http.Handler) http.Handler {
	policies := &policyCache{store: opts.Store, safetyWords: opts.SafetyWords, safetyIPs: opts.SafetyIPs}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawBody := []byte(nil)
			bodyAvailable := requestBodyShouldBeRead(r)
			if bodyAvailable {
				body, ok := readAndRestoreBody(w, r)
				if !ok {
					return
				}
				rawBody = body
			}

			meta := requestmeta.Metadata{
				ClientIP:       requestmeta.ClientIP(r),
				ConversationID: requestmeta.ConversationID(r, rawBody),
			}
			r = r.WithContext(context.WithValue(r.Context(), metadataContextKey, meta))

			p := policies.load()
			if d := p.evaluate(r, rawBody, meta); d.blocked {
				recordBlockedHistory(opts.ChatHistory, rawBody, meta, d)
				writeBlocked(w, p.blockMessage, d)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func FromContext(ctx context.Context) (requestmeta.Metadata, bool) {
	if ctx == nil {
		return requestmeta.Metadata{}, false
	}
	meta, ok := ctx.Value(metadataContextKey).(requestmeta.Metadata)
	return meta, ok
}

func requestBodyShouldBeRead(r *http.Request) bool {
	if r == nil || r.Body == nil || r.Body == http.NoBody {
		return false
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch {
		return false
	}
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if strings.HasPrefix(contentType, "multipart/") || strings.HasPrefix(contentType, "application/octet-stream") {
		return false
	}
	return responsecache.CacheableRequest(r) || strings.Contains(contentType, "json") || strings.Contains(contentType, "+json")
}

func readAndRestoreBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	rawBody, err := io.ReadAll(io.LimitReader(r.Body, maxScanBodyBytes+1))
	if closeErr := r.Body.Close(); closeErr != nil {
		config.Logger.Warn("[request_guard] close request body failed", "error", closeErr)
	}
	if err != nil {
		http.Error(w, "read request body failed", http.StatusBadRequest)
		return nil, false
	}
	if len(rawBody) > maxScanBodyBytes {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return nil, false
	}
	r.Body = io.NopCloser(bytes.NewReader(rawBody))
	return rawBody, true
}

func (c *policyCache) load() policy {
	cfg := config.SafetyConfig{}
	if c != nil && c.store != nil {
		cfg = c.store.SafetyConfig()
	}
	cfg = mergeSafetySources(cfg, c)
	signature := safetyConfigSignature(cfg)
	if c != nil {
		c.mu.Lock()
		if signature == c.signature {
			p := c.cached
			c.mu.Unlock()
			return p
		}
		p := buildPolicy(cfg)
		c.signature = signature
		c.cached = p
		c.mu.Unlock()
		return p
	}
	return buildPolicy(cfg)
}

// mergeSafetySources combines the legacy config.SafetyConfig lists with the
// dedicated SQLite stores. SQLite values are appended to the config-supplied
// values so an upgrade is non-destructive: data that lives in either source
// (or both) reaches the policy. Duplicates are removed in buildPolicy via
// the existing dedupe-by-string normalization.
func mergeSafetySources(cfg config.SafetyConfig, c *policyCache) config.SafetyConfig {
	if c == nil {
		return cfg
	}
	if c.safetyWords != nil {
		if content, regex, jail, err := c.safetyWords.Snapshot(); err == nil {
			cfg.BannedContent = appendUnique(cfg.BannedContent, content)
			cfg.BannedRegex = appendUnique(cfg.BannedRegex, regex)
			cfg.Jailbreak.Patterns = appendUnique(cfg.Jailbreak.Patterns, jail)
		}
	}
	if c.safetyIPs != nil {
		if blocked, _, blockedConv, err := c.safetyIPs.Snapshot(); err == nil {
			// allowed_ips is reserved for future opt-in allowlist semantics
			// and is intentionally not yet consumed by buildPolicy.
			cfg.BlockedIPs = appendUnique(cfg.BlockedIPs, blocked)
			cfg.BlockedConversationIDs = appendUnique(cfg.BlockedConversationIDs, blockedConv)
		}
	}
	return cfg
}

func appendUnique(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	seen := make(map[string]struct{}, len(base)+len(extra))
	for _, v := range base {
		seen[v] = struct{}{}
	}
	out := append([]string(nil), base...)
	for _, v := range extra {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func safetyConfigSignature(cfg config.SafetyConfig) string {
	b, err := json.Marshal(cfg)
	if err != nil {
		return ""
	}
	return string(b)
}

func buildPolicy(cfg config.SafetyConfig) policy {
	enabled := false
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}
	p := policy{
		enabled:                enabled,
		blockMessage:           strings.TrimSpace(cfg.BlockMessage),
		blockedConversationIDs: map[string]struct{}{},
	}
	if p.blockMessage == "" {
		p.blockMessage = defaultBlockMessage
	}
	if !p.enabled {
		return p
	}
	for _, raw := range cfg.BlockedIPs {
		if matcher, ok := parseIPMatcher(raw); ok {
			p.blockedIPs = append(p.blockedIPs, matcher)
		}
	}
	for _, id := range cfg.BlockedConversationIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			p.blockedConversationIDs[strings.ToLower(id)] = struct{}{}
		}
	}
	for _, item := range cfg.BannedContent {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" {
			p.bannedContent = append(p.bannedContent, item)
		}
	}
	for _, pattern := range cfg.BannedRegex {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if re, err := regexp.Compile(pattern); err == nil {
			p.bannedRegex = append(p.bannedRegex, re)
		}
	}
	if cfg.Jailbreak.Enabled != nil {
		p.jailbreakEnabled = *cfg.Jailbreak.Enabled
	} else {
		// default-on when safety is enabled and operator left jailbreak unspecified
		p.jailbreakEnabled = true
	}
	for _, item := range append(defaultJailbreakPatterns, cfg.Jailbreak.Patterns...) {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" {
			p.jailbreakPatterns = append(p.jailbreakPatterns, item)
		}
	}
	return p
}

func parseIPMatcher(raw string) (ipMatcher, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ipMatcher{}, false
	}
	if ip := net.ParseIP(raw); ip != nil {
		return ipMatcher{raw: raw, ip: ip}, true
	}
	if _, cidr, err := net.ParseCIDR(raw); err == nil {
		return ipMatcher{raw: raw, cidr: cidr}, true
	}
	return ipMatcher{}, false
}

func (p policy) evaluate(r *http.Request, body []byte, meta requestmeta.Metadata) decision {
	if !p.enabled {
		return decision{}
	}
	if p.ipBlocked(meta.ClientIP) {
		return decision{blocked: true, code: "ip_blocked", detail: "request ip is blocked"}
	}
	if meta.ConversationID != "" {
		if _, ok := p.blockedConversationIDs[strings.ToLower(meta.ConversationID)]; ok {
			return decision{blocked: true, code: "conversation_blocked", detail: "conversation id is blocked"}
		}
	}
	if isContentScanExempt(r) {
		return decision{}
	}
	if len(body) == 0 || !requestBodyShouldBeRead(r) {
		return decision{}
	}
	text := strings.ToLower(extractRequestText(body))
	if text == "" {
		return decision{}
	}
	for _, needle := range p.bannedContent {
		if strings.Contains(text, needle) {
			return decision{blocked: true, code: "content_blocked", detail: "request content matched banned content"}
		}
	}
	for _, re := range p.bannedRegex {
		if re.MatchString(text) {
			return decision{blocked: true, code: "content_regex_blocked", detail: "request content matched banned regex"}
		}
	}
	if p.jailbreakEnabled {
		for _, needle := range p.jailbreakPatterns {
			if strings.Contains(text, needle) {
				return decision{blocked: true, code: "jailbreak_blocked", detail: "request content matched jailbreak policy"}
			}
		}
	}
	return decision{}
}

func (p policy) ipBlocked(rawIP string) bool {
	ip := net.ParseIP(strings.TrimSpace(rawIP))
	if ip == nil {
		return false
	}
	for _, matcher := range p.blockedIPs {
		if matcher.ip != nil && matcher.ip.Equal(ip) {
			return true
		}
		if matcher.cidr != nil && matcher.cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// isContentScanExempt skips body content scanning for admin / webui / health
// paths. IP and conversation-id blocking still apply globally; only the
// keyword/regex/jailbreak scan is bypassed so operators cannot lock themselves
// out by configuring a banned word that legitimately appears in their own
// settings payload.
func isContentScanExempt(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}
	path := r.URL.Path
	switch {
	case path == "/healthz", path == "/readyz":
		return true
	case path == "/admin", strings.HasPrefix(path, "/admin/"):
		return true
	case path == "/webui", strings.HasPrefix(path, "/webui/"):
		return true
	case strings.HasPrefix(path, "/static/"), strings.HasPrefix(path, "/assets/"):
		return true
	}
	return false
}

func extractRequestText(body []byte) string {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return string(body)
	}
	var out strings.Builder
	collectText(&out, value, 0)
	return out.String()
}

func collectText(out *strings.Builder, value any, depth int) {
	if out.Len() >= maxCollectedText || depth > 12 {
		return
	}
	switch v := value.(type) {
	case string:
		appendText(out, v)
	case map[string]any:
		// Recurse into all values so that tool_result / functionResponse /
		// nested tool blocks are scanned even when their container key is not
		// in the legacy whitelist. depth + maxCollectedText still bound the
		// traversal so this is bounded.
		for _, item := range v {
			collectText(out, item, depth+1)
		}
	case []any:
		for _, item := range v {
			collectText(out, item, depth+1)
		}
	}
}

func appendText(out *strings.Builder, text string) {
	text = strings.TrimSpace(text)
	if text == "" || out.Len() >= maxCollectedText {
		return
	}
	if out.Len() > 0 {
		out.WriteByte('\n')
	}
	remaining := maxCollectedText - out.Len()
	if len(text) > remaining {
		text = text[:remaining]
	}
	out.WriteString(text)
}

func recordBlockedHistory(store *chathistory.Store, body []byte, meta requestmeta.Metadata, d decision) {
	if store == nil || !store.Enabled() {
		return
	}
	now := time.Now()
	userInput := extractRequestText(body)
	if len(userInput) > 2048 {
		userInput = userInput[:2048]
	}
	model := modelFromBody(body)
	entry, err := store.Start(chathistory.StartParams{
		Status:         "error",
		Model:          model,
		UserInput:      userInput,
		RequestIP:      meta.ClientIP,
		ConversationID: meta.ConversationID,
	})
	if err != nil || entry.ID == "" {
		return
	}
	_, err = store.Update(entry.ID, chathistory.UpdateParams{
		Status:       "error",
		Error:        d.detail,
		StatusCode:   http.StatusForbidden,
		ElapsedMs:    time.Since(now).Milliseconds(),
		FinishReason: "policy_blocked",
		Completed:    true,
	})
	if err != nil {
		config.Logger.Warn("[request_guard] record blocked history failed", "error", err)
	}
}

func modelFromBody(body []byte) string {
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return ""
	}
	if model, ok := obj["model"].(string); ok {
		return strings.TrimSpace(model)
	}
	return ""
}

func writeBlocked(w http.ResponseWriter, message string, d decision) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusForbidden)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"detail": message,
		"code":   d.code,
		"reason": d.detail,
	}); err != nil {
		config.Logger.Warn("[request_guard] write blocked response failed", "error", err)
	}
}
