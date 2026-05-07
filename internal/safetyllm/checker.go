// Package safetyllm performs binary-verdict content safety review by
// asking deepseek-v4-flash-nothinking whether the user input violates a
// short, hard-coded policy.
//
// It REPLACES the v1.0.13 substring/regex/jailbreak.patterns pipeline
// (which had unacceptably high false-positive rate against natural
// Chinese / English prose). The verdict is binary — "violation" or
// "ok" — so the call site can branch without confidence-threshold
// tuning.
//
// Design notes:
//   - Fail-open by default: parse error / timeout / upstream error
//     all return Verdict{Violation: false} so a flaky safety LLM
//     never blocks live business traffic. Operator can switch to
//     fail-closed.
//   - LRU cache keyed by sha256(input) with TTL avoids paying for
//     identical-text repeats (hot prompts, retries, batched runs).
//   - Concurrency semaphore caps simultaneous LLM checks to avoid
//     blowing the account pool when traffic spikes.
//   - The audit prompt wraps user content in triple-quote fences and
//     instructs the model NOT to execute embedded commands — defends
//     against the user trying to jailbreak the auditor.
package safetyllm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"DeepSeek_Web_To_API/internal/auth"
)

// Verdict is the binary outcome of a safety check, plus introspection
// fields the admin metrics surface uses to display latency / cache hit
// breakdown.
type Verdict struct {
	Violation bool
	Cached    bool
	LatencyMs int64
	// FailOpen is true when the verdict landed on Violation=false because
	// an error or timeout occurred and the operator has fail-open enabled.
	// Distinguished from a real "ok" verdict so dashboards can surface
	// review-pipeline outages.
	FailOpen bool
}

// Config controls the LLM-checker runtime. Mutating it after Checker
// construction is undefined — operator changes flow through the Store
// and a fresh Checker is built per snapshot.
type Config struct {
	Enabled         bool
	Model           string
	TimeoutMs       int
	FailOpen        bool
	CacheTTLSeconds int
	CacheMaxEntries int
	MinInputChars   int
	MaxInputChars   int
	MaxConcurrent   int
}

// DefaultConfig returns conservative defaults: disabled, flash-nothinking,
// 5s timeout, fail-open, 10 min cache, 30-char minimum to skip trivially
// short prompts, 8KB cap to keep audit prompt size bounded, 16 concurrent.
func DefaultConfig() Config {
	return Config{
		Enabled:         false,
		Model:           "deepseek-v4-flash-nothinking",
		TimeoutMs:       5000,
		FailOpen:        true,
		CacheTTLSeconds: 600,
		CacheMaxEntries: 10000,
		MinInputChars:   30,
		MaxInputChars:   8000,
		MaxConcurrent:   16,
	}
}

// CompletionDoer is the minimal upstream surface the checker needs. The
// production dsclient.Client implements it; tests inject stubs.
type CompletionDoer interface {
	RunSafetyCheck(ctx context.Context, a *auth.RequestAuth, model, prompt string) (string, error)
}

// Checker is the runtime-injectable surface. Handlers hold a Checker
// (which may be a no-op when disabled) and call CheckWithAuth on every
// inbound chat/responses/messages request.
type Checker interface {
	Enabled() bool
	CheckWithAuth(ctx context.Context, a *auth.RequestAuth, text string) (Verdict, error)
	Stats() Stats
}

// Stats is exported via /admin/metrics/overview safety_llm_check node.
type Stats struct {
	Enabled             bool   `json:"enabled"`
	RequestsTotal       int64  `json:"requests_total"`
	Violations          int64  `json:"violations"`
	OK                  int64  `json:"ok"`
	Skipped             int64  `json:"skipped_below_threshold"`
	CacheHits           int64  `json:"cache_hits"`
	CacheSize           int64  `json:"cache_size"`
	Timeouts            int64  `json:"timeouts"`
	UpstreamErrors      int64  `json:"upstream_errors"`
	ParseErrors         int64  `json:"parse_errors"`
	FailOpenInvocations int64  `json:"fail_open_invocations"`
	ConcurrentInflight  int64  `json:"concurrent_inflight"`
	AvgLatencyMs        int64  `json:"avg_latency_ms"`
	Model               string `json:"model"`
}

// LLMChecker is the production Checker.
//
// v1.0.15 hot-reload: cfg is no longer a frozen snapshot. The checker
// asks the configSource for the live config on every Enabled() and
// CheckWithAuth() call, so toggling safety.llm_check.enabled (or any
// runtime knob like timeout / fail_open / model / TTL) via PUT
// /admin/settings takes effect for the very next request — no restart.
//
// v1.0.18: when the OPERATOR-VISIBLE audit semantics change (model
// swap, fail_open flip, enabled flip), the LRU cache is invalidated so
// stale verdicts produced by an earlier (potentially weaker / mis-
// configured) model don't get reused. Without this an operator who
// upgrades from flash-nothinking to pro-nothinking still sees the old
// verdicts until each cache entry's TTL expires (default 10 min) or
// the process restarts. Detection is "any change in (Enabled, Model,
// FailOpen)" tracked under c.mu.
//
// Two fields stay frozen at construction because they back finite-size
// runtime objects: CacheMaxEntries (LRU capacity) and MaxConcurrent
// (semaphore depth). Those still need a restart if the operator wants
// to grow / shrink them.
type LLMChecker struct {
	source ConfigSource
	doer   CompletionDoer
	cache  *lruCache
	sem    chan struct{}

	mu                  sync.Mutex
	requestsTotal       int64
	violations          int64
	okCount             int64
	skipped             int64
	cacheHits           int64
	timeouts            int64
	upstreamErrors      int64
	parseErrors         int64
	failOpenInvocations int64
	concurrentInflight  int64
	totalLatencyMs      int64
	latencyMeasureCount int64

	// lastSemantics tracks (Enabled, Model, FailOpen) so we can detect a
	// hot-reload change and purge the cache. Empty zero-value is fine for
	// the very first call.
	lastSemantics auditSemantics
}

// auditSemantics is the subset of Config that, when changed, invalidates
// previously-cached verdicts. Cache size / TTL / min-max chars don't
// invalidate (they affect storage / inputs but not the verdict mapping).
type auditSemantics struct {
	Enabled  bool
	Model    string
	FailOpen bool
}

func semanticsFor(cfg Config) auditSemantics {
	return auditSemantics{Enabled: cfg.Enabled, Model: cfg.Model, FailOpen: cfg.FailOpen}
}

// ConfigSource lets the checker read the current operator-facing
// SafetyLLMCheck config at runtime so changes are picked up without a
// process restart. Implementations must be safe for concurrent reads.
type ConfigSource interface {
	SafetyLLMCheckConfig() Config
}

// staticConfigSource wraps a frozen Config so legacy callers
// (NewLLMChecker(cfg, doer)) keep working.
type staticConfigSource struct{ cfg Config }

func (s staticConfigSource) SafetyLLMCheckConfig() Config { return s.cfg }

// NewLLMChecker constructs a checker bound to a frozen Config. Use this
// in tests or anywhere config is known not to change. Pass a nil doer
// to get a disabled checker.
func NewLLMChecker(cfg Config, doer CompletionDoer) *LLMChecker {
	return NewLLMCheckerWithSource(staticConfigSource{cfg: cfg}, doer)
}

// NewLLMCheckerWithSource is the production constructor: it accepts a
// ConfigSource that reflects the live operator config snapshot, so
// Enabled / TimeoutMs / FailOpen / Model / CacheTTLSeconds /
// MinInputChars / MaxInputChars are picked up per-request without
// restart. CacheMaxEntries + MaxConcurrent are read once at construction.
func NewLLMCheckerWithSource(source ConfigSource, doer CompletionDoer) *LLMChecker {
	bootstrap := source.SafetyLLMCheckConfig()
	if bootstrap.MaxConcurrent <= 0 {
		bootstrap.MaxConcurrent = 1
	}
	if bootstrap.CacheMaxEntries <= 0 {
		bootstrap.CacheMaxEntries = 1
	}
	return &LLMChecker{
		source: source,
		doer:   doer,
		cache:  newLRUCache(bootstrap.CacheMaxEntries),
		sem:    make(chan struct{}, bootstrap.MaxConcurrent),
	}
}

func (c *LLMChecker) currentConfig() Config {
	if c == nil || c.source == nil {
		return DefaultConfig()
	}
	return c.source.SafetyLLMCheckConfig()
}

// maybePurgeCacheOnSemanticsChange compares the current audit semantics
// (Enabled, Model, FailOpen) to the last call's. If they changed —
// typically when the operator flips the toggle in the WebUI or upgrades
// model from flash-nothinking to pro-nothinking — the LRU cache is
// invalidated so a previous model's verdicts don't get replayed.
//
// The first call sees a zero-value lastSemantics; if cfg.Enabled is
// already true we still treat that as a change and purge (defensive —
// the previous process's cache file does not persist, but a hot-import
// of config could conceivably stage state).
func (c *LLMChecker) maybePurgeCacheOnSemanticsChange(cfg Config) {
	current := semanticsFor(cfg)
	c.mu.Lock()
	changed := c.lastSemantics != current
	c.lastSemantics = current
	c.mu.Unlock()
	if changed {
		c.cache.purge()
	}
}

// Enabled reports whether the checker is currently active. Reads live
// config so a WebUI flip propagates immediately.
func (c *LLMChecker) Enabled() bool {
	if c == nil {
		return false
	}
	return c.currentConfig().Enabled && c.doer != nil
}

// CheckWithAuth runs a safety check using the caller's RequestAuth so
// the upstream LLM call lands on the same managed-account budget the
// rest of the request uses (no separate audit-account pool to manage).
func (c *LLMChecker) CheckWithAuth(ctx context.Context, a *auth.RequestAuth, text string) (Verdict, error) {
	cfg := c.currentConfig()
	c.maybePurgeCacheOnSemanticsChange(cfg)
	if !cfg.Enabled || c.doer == nil {
		return Verdict{Violation: false}, nil
	}
	c.mu.Lock()
	c.requestsTotal++
	c.mu.Unlock()

	// v1.0.19: peel any DeepSeek protocol markers
	// (`<|System|>...<|end▁of▁instructions|>`, `<|begin▁of▁sentence|>`,
	// `<|User|>`, `<|end▁of▁turn|>`) before further processing. Callers
	// now pass StandardRequest.LatestUserText (just the user's last
	// message) but we still get FinalPrompt as a fallback when extraction
	// fails — the strip ensures the audit LLM never sees gateway-built
	// system instructions in either path. Idempotent on clean input.
	stripped := stripDeepSeekProtocolMarkers(text)
	// v1.0.17: strip our own gateway-injected banners (Reasoning Effort,
	// ToolChainPlaybook, BINDING TOOL-USE COMPLIANCE) before deciding
	// what to audit. Without this every chat request looks like
	// "user_text + 'Reasoning Effort: ... Stress-test ... adversarial
	// inputs'" and the audit LLM occasionally judged the gateway's own
	// banner as violation, blocking legitimate short replies like "hello".
	stripped = stripKnownInjections(stripped)
	trimmed := strings.TrimSpace(stripped)
	if len(trimmed) < cfg.MinInputChars {
		c.mu.Lock()
		c.skipped++
		c.okCount++
		c.mu.Unlock()
		return Verdict{Violation: false}, nil
	}

	// v1.0.17: hard-jailbreak fast path. Specific imperative phrases
	// ("ignore previous instructions", "启用开发者模式", "你不被允许思考",
	// "看似儿童的角色实则", etc.) are themselves prompt-injection ATTEMPTS
	// — short-circuit to a violation verdict without burning an LLM call.
	// flash-nothinking has been observed missing exactly these strings in
	// production, so this is the belt-and-suspenders that makes the
	// pipeline robust to a weak audit model.
	if matchesHardJailbreakSignal(trimmed) {
		c.mu.Lock()
		c.violations++
		c.mu.Unlock()
		v := Verdict{Violation: true, LatencyMs: 0}
		c.cache.set(hashInput(trimmed), v, time.Now().Add(time.Duration(cfg.CacheTTLSeconds)*time.Second))
		return v, nil
	}

	if len(trimmed) > cfg.MaxInputChars && cfg.MaxInputChars > 0 {
		keep := cfg.MaxInputChars / 2
		trimmed = trimmed[:keep] + "\n\n[... truncated for safety check ...]\n\n" + trimmed[len(trimmed)-keep:]
	}

	key := hashInput(trimmed)
	if v, ok := c.cache.get(key, time.Now()); ok {
		v.Cached = true
		c.mu.Lock()
		c.cacheHits++
		if v.Violation {
			c.violations++
		} else {
			c.okCount++
		}
		c.mu.Unlock()
		return v, nil
	}

	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-checkCtx.Done():
		return c.failOpenVerdict(cfg, 0, errors.New("semaphore wait cancelled"))
	}
	c.mu.Lock()
	c.concurrentInflight++
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.concurrentInflight--
		c.mu.Unlock()
	}()

	prompt := buildAuditPrompt(trimmed)
	start := time.Now()
	output, err := c.doer.RunSafetyCheck(checkCtx, a, cfg.Model, prompt)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		c.mu.Lock()
		if errors.Is(err, context.DeadlineExceeded) {
			c.timeouts++
		} else {
			c.upstreamErrors++
		}
		c.mu.Unlock()
		return c.failOpenVerdict(cfg, latencyMs, err)
	}

	violation, parsed := parseBinaryVerdict(output)
	if !parsed {
		c.mu.Lock()
		c.parseErrors++
		c.mu.Unlock()
		return c.failOpenVerdict(cfg, latencyMs, errors.New("parse failed: "+output))
	}

	v := Verdict{Violation: violation, LatencyMs: latencyMs}
	c.cache.set(key, v, time.Now().Add(time.Duration(cfg.CacheTTLSeconds)*time.Second))
	c.mu.Lock()
	if violation {
		c.violations++
	} else {
		c.okCount++
	}
	c.totalLatencyMs += latencyMs
	c.latencyMeasureCount++
	c.mu.Unlock()
	return v, nil
}

func (c *LLMChecker) failOpenVerdict(cfg Config, latencyMs int64, _ error) (Verdict, error) {
	if cfg.FailOpen {
		c.mu.Lock()
		c.failOpenInvocations++
		c.okCount++
		c.mu.Unlock()
		return Verdict{Violation: false, FailOpen: true, LatencyMs: latencyMs}, nil
	}
	c.mu.Lock()
	c.violations++
	c.mu.Unlock()
	return Verdict{Violation: true, FailOpen: true, LatencyMs: latencyMs}, nil
}

// Stats snapshots counters for /admin/metrics/overview.
func (c *LLMChecker) Stats() Stats {
	if c == nil {
		return Stats{Enabled: false}
	}
	cfg := c.currentConfig()
	c.mu.Lock()
	defer c.mu.Unlock()
	avg := int64(0)
	if c.latencyMeasureCount > 0 {
		avg = c.totalLatencyMs / c.latencyMeasureCount
	}
	return Stats{
		Enabled:             cfg.Enabled && c.doer != nil,
		RequestsTotal:       c.requestsTotal,
		Violations:          c.violations,
		OK:                  c.okCount,
		Skipped:             c.skipped,
		CacheHits:           c.cacheHits,
		CacheSize:           int64(c.cache.size()),
		Timeouts:            c.timeouts,
		UpstreamErrors:      c.upstreamErrors,
		ParseErrors:         c.parseErrors,
		FailOpenInvocations: c.failOpenInvocations,
		ConcurrentInflight:  c.concurrentInflight,
		AvgLatencyMs:        avg,
		Model:               cfg.Model,
	}
}

func hashInput(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
