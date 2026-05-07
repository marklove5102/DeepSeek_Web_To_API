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
	Enabled            bool   `json:"enabled"`
	RequestsTotal      int64  `json:"requests_total"`
	Violations         int64  `json:"violations"`
	OK                 int64  `json:"ok"`
	Skipped            int64  `json:"skipped_below_threshold"`
	CacheHits          int64  `json:"cache_hits"`
	CacheSize          int64  `json:"cache_size"`
	Timeouts           int64  `json:"timeouts"`
	UpstreamErrors     int64  `json:"upstream_errors"`
	ParseErrors        int64  `json:"parse_errors"`
	FailOpenInvocations int64 `json:"fail_open_invocations"`
	ConcurrentInflight int64  `json:"concurrent_inflight"`
	AvgLatencyMs       int64  `json:"avg_latency_ms"`
	Model              string `json:"model"`
}

// LLMChecker is the production Checker.
type LLMChecker struct {
	cfg    Config
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
}

// NewLLMChecker constructs an LLMChecker. Pass a nil doer to get a
// disabled checker (always returns Verdict{Violation: false} without
// touching upstream).
func NewLLMChecker(cfg Config, doer CompletionDoer) *LLMChecker {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 1
	}
	if cfg.CacheMaxEntries <= 0 {
		cfg.CacheMaxEntries = 1
	}
	return &LLMChecker{
		cfg:   cfg,
		doer:  doer,
		cache: newLRUCache(cfg.CacheMaxEntries),
		sem:   make(chan struct{}, cfg.MaxConcurrent),
	}
}

// Enabled reports whether the checker is currently active.
func (c *LLMChecker) Enabled() bool {
	if c == nil {
		return false
	}
	return c.cfg.Enabled && c.doer != nil
}

// CheckWithAuth runs a safety check using the caller's RequestAuth so
// the upstream LLM call lands on the same managed-account budget the
// rest of the request uses (no separate audit-account pool to manage).
func (c *LLMChecker) CheckWithAuth(ctx context.Context, a *auth.RequestAuth, text string) (Verdict, error) {
	if !c.Enabled() {
		return Verdict{Violation: false}, nil
	}
	c.mu.Lock()
	c.requestsTotal++
	c.mu.Unlock()

	trimmed := strings.TrimSpace(text)
	if len(trimmed) < c.cfg.MinInputChars {
		c.mu.Lock()
		c.skipped++
		c.okCount++
		c.mu.Unlock()
		return Verdict{Violation: false}, nil
	}
	if len(trimmed) > c.cfg.MaxInputChars {
		// Truncate to bound prompt size; keep the head + tail because
		// jailbreak attempts often live in the tail.
		keep := c.cfg.MaxInputChars / 2
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

	timeout := time.Duration(c.cfg.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Acquire semaphore slot or fall through fail-open if the context
	// dies waiting (we never want safety review to wedge live traffic).
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-checkCtx.Done():
		return c.failOpenVerdict(0, errors.New("semaphore wait cancelled"))
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
	output, err := c.doer.RunSafetyCheck(checkCtx, a, c.cfg.Model, prompt)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		c.mu.Lock()
		if errors.Is(err, context.DeadlineExceeded) {
			c.timeouts++
		} else {
			c.upstreamErrors++
		}
		c.mu.Unlock()
		return c.failOpenVerdict(latencyMs, err)
	}

	violation, parsed := parseBinaryVerdict(output)
	if !parsed {
		c.mu.Lock()
		c.parseErrors++
		c.mu.Unlock()
		return c.failOpenVerdict(latencyMs, errors.New("parse failed: "+output))
	}

	v := Verdict{Violation: violation, LatencyMs: latencyMs}
	c.cache.set(key, v, time.Now().Add(time.Duration(c.cfg.CacheTTLSeconds)*time.Second))
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

func (c *LLMChecker) failOpenVerdict(latencyMs int64, _ error) (Verdict, error) {
	if c.cfg.FailOpen {
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
	c.mu.Lock()
	defer c.mu.Unlock()
	avg := int64(0)
	if c.latencyMeasureCount > 0 {
		avg = c.totalLatencyMs / c.latencyMeasureCount
	}
	return Stats{
		Enabled:             c.cfg.Enabled && c.doer != nil,
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
		Model:               c.cfg.Model,
	}
}

func hashInput(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
