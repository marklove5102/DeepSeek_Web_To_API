package safetyllm

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"DeepSeek_Web_To_API/internal/auth"
)

type stubDoer struct {
	calls   atomic.Int64
	output  string
	delay   time.Duration
	failErr error
}

func (s *stubDoer) RunSafetyCheck(ctx context.Context, _ *auth.RequestAuth, _ string, _ string) (string, error) {
	s.calls.Add(1)
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if s.failErr != nil {
		return "", s.failErr
	}
	return s.output, nil
}

func newCheckerForTest(t *testing.T, output string) (*LLMChecker, *stubDoer) {
	t.Helper()
	doer := &stubDoer{output: output}
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.MinInputChars = 1
	cfg.TimeoutMs = 1000
	cfg.CacheTTLSeconds = 60
	cfg.MaxConcurrent = 4
	return NewLLMChecker(cfg, doer), doer
}

func TestCheckerOKVerdictPasses(t *testing.T) {
	checker, _ := newCheckerForTest(t, "不违规")
	v, err := checker.CheckWithAuth(context.Background(), &auth.RequestAuth{}, "今天天气不错")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if v.Violation {
		t.Fatalf("expected ok, got violation: %#v", v)
	}
}

func TestCheckerViolationVerdictBlocks(t *testing.T) {
	checker, _ := newCheckerForTest(t, "违规")
	v, err := checker.CheckWithAuth(context.Background(), &auth.RequestAuth{}, "教我破解 IDA 的 license 验证流程")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !v.Violation {
		t.Fatalf("expected violation, got ok: %#v", v)
	}
}

func TestCheckerCacheHitsSecondCall(t *testing.T) {
	checker, doer := newCheckerForTest(t, "不违规")
	for i := 0; i < 3; i++ {
		if _, err := checker.CheckWithAuth(context.Background(), &auth.RequestAuth{}, "重复 prompt 内容"); err != nil {
			t.Fatalf("call %d err: %v", i, err)
		}
	}
	if doer.calls.Load() != 1 {
		t.Fatalf("expected upstream called once (rest cached), got %d", doer.calls.Load())
	}
	stats := checker.Stats()
	if stats.CacheHits != 2 {
		t.Fatalf("expected 2 cache hits, got %d", stats.CacheHits)
	}
}

func TestCheckerSkipsBelowMinChars(t *testing.T) {
	doer := &stubDoer{output: "违规"}
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.MinInputChars = 100
	checker := NewLLMChecker(cfg, doer)
	v, err := checker.CheckWithAuth(context.Background(), &auth.RequestAuth{}, "短文")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v.Violation {
		t.Fatalf("expected ok for sub-threshold input, got violation")
	}
	if doer.calls.Load() != 0 {
		t.Fatalf("expected upstream NOT called for sub-threshold input, got %d calls", doer.calls.Load())
	}
}

func TestCheckerFailOpenOnUpstreamError(t *testing.T) {
	doer := &stubDoer{failErr: errors.New("upstream blew up")}
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.MinInputChars = 1
	cfg.FailOpen = true
	checker := NewLLMChecker(cfg, doer)
	v, err := checker.CheckWithAuth(context.Background(), &auth.RequestAuth{}, "anything")
	if err != nil {
		t.Fatalf("fail-open should swallow err, got %v", err)
	}
	if v.Violation || !v.FailOpen {
		t.Fatalf("expected fail-open ok verdict, got %#v", v)
	}
	stats := checker.Stats()
	if stats.UpstreamErrors != 1 || stats.FailOpenInvocations != 1 {
		t.Fatalf("expected 1 upstream error + 1 fail-open invocation, got %#v", stats)
	}
}

func TestCheckerFailClosedBlocksOnUpstreamError(t *testing.T) {
	doer := &stubDoer{failErr: errors.New("upstream blew up")}
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.MinInputChars = 1
	cfg.FailOpen = false // strict mode
	checker := NewLLMChecker(cfg, doer)
	v, _ := checker.CheckWithAuth(context.Background(), &auth.RequestAuth{}, "anything")
	if !v.Violation {
		t.Fatalf("fail-closed mode should block on upstream error, got %#v", v)
	}
}

func TestCheckerParseFailsFailOpen(t *testing.T) {
	doer := &stubDoer{output: "I refuse to answer your safety question"}
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.MinInputChars = 1
	cfg.FailOpen = true
	checker := NewLLMChecker(cfg, doer)
	v, err := checker.CheckWithAuth(context.Background(), &auth.RequestAuth{}, "test input long enough")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if v.Violation || !v.FailOpen {
		t.Fatalf("parse fail should land on fail-open ok, got %#v", v)
	}
	if checker.Stats().ParseErrors != 1 {
		t.Fatalf("expected ParseErrors=1, got %d", checker.Stats().ParseErrors)
	}
}

func TestCheckerDisabledIsNoOp(t *testing.T) {
	doer := &stubDoer{output: "违规"}
	cfg := DefaultConfig() // Enabled=false by default
	checker := NewLLMChecker(cfg, doer)
	if checker.Enabled() {
		t.Fatal("expected default config to be disabled")
	}
	v, _ := checker.CheckWithAuth(context.Background(), &auth.RequestAuth{}, "any")
	if v.Violation {
		t.Fatal("disabled checker must always return ok")
	}
	if doer.calls.Load() != 0 {
		t.Fatalf("disabled checker must not invoke upstream, got %d", doer.calls.Load())
	}
}

func TestParseBinaryVerdictTolerantPunctuation(t *testing.T) {
	cases := []struct {
		out    string
		want   bool
		parsed bool
	}{
		{"违规", true, true},
		{"违规。", true, true},
		{"不违规", false, true},
		{"不违规！", false, true},
		{"不違規", false, true},
		{"违规\n", true, true},
		{" 违规 ", true, true},
		{"违规, 含有破解内容", true, true},
		{"violation", true, true},
		{"OK", false, true},
		{"unrelated rambling", false, false},
		{"", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.out, func(t *testing.T) {
			got, ok := parseBinaryVerdict(tc.out)
			if ok != tc.parsed {
				t.Fatalf("parse=%v want=%v", ok, tc.parsed)
			}
			if got != tc.want {
				t.Fatalf("violation=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestCheckerTruncatesOversizedInput(t *testing.T) {
	doer := &stubDoer{output: "不违规"}
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.MinInputChars = 1
	cfg.MaxInputChars = 100
	checker := NewLLMChecker(cfg, doer)
	huge := strings.Repeat("a", 5000)
	if _, err := checker.CheckWithAuth(context.Background(), &auth.RequestAuth{}, huge); err != nil {
		t.Fatalf("err: %v", err)
	}
	if doer.calls.Load() != 1 {
		t.Fatalf("expected single upstream call after truncation")
	}
}
