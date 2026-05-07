package shared

import (
	"context"
	"errors"
	"testing"

	dsclient "DeepSeek_Web_To_API/internal/deepseek/client"
)

type sessionDeleterStub struct {
	singleCalls   int
	allCalls      int
	lastSessionID string
	lastCtxErr    error
	singleErr     error
	allErr        error
}

func (s *sessionDeleterStub) DeleteSessionForToken(ctx context.Context, _ string, sessionID string) (*dsclient.DeleteSessionResult, error) {
	s.singleCalls++
	s.lastSessionID = sessionID
	s.lastCtxErr = ctx.Err()
	if s.singleErr != nil {
		return nil, s.singleErr
	}
	return &dsclient.DeleteSessionResult{SessionID: sessionID, Success: true}, nil
}

func (s *sessionDeleterStub) DeleteAllSessionsForToken(ctx context.Context, _ string) error {
	s.allCalls++
	s.lastCtxErr = ctx.Err()
	return s.allErr
}

func TestAutoDeleteRemoteSessionDispatchesPerMode(t *testing.T) {
	tests := []struct {
		name       string
		mode       string
		sessionID  string
		token      string
		wantSingle int
		wantAll    int
	}{
		{name: "none-mode-skips", mode: "none", sessionID: "s1", token: "t", wantSingle: 0, wantAll: 0},
		{name: "empty-mode-skips", mode: "", sessionID: "s1", token: "t", wantSingle: 0, wantAll: 0},
		{name: "empty-token-skips", mode: "all", sessionID: "s1", token: "", wantSingle: 0, wantAll: 0},
		{name: "single-fires-once", mode: "single", sessionID: "s1", token: "t", wantSingle: 1, wantAll: 0},
		{name: "single-with-empty-session-id-skips", mode: "single", sessionID: "", token: "t", wantSingle: 0, wantAll: 0},
		{name: "all-fires-once", mode: "all", sessionID: "anything", token: "t", wantSingle: 0, wantAll: 1},
		{name: "unknown-mode-warns-only", mode: "weird", sessionID: "s1", token: "t", wantSingle: 0, wantAll: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ds := &sessionDeleterStub{}
			AutoDeleteRemoteSession(context.Background(), ds, tc.mode, "acct", tc.token, tc.sessionID)
			if ds.singleCalls != tc.wantSingle {
				t.Fatalf("single calls=%d want=%d", ds.singleCalls, tc.wantSingle)
			}
			if ds.allCalls != tc.wantAll {
				t.Fatalf("all calls=%d want=%d", ds.allCalls, tc.wantAll)
			}
		})
	}
}

func TestAutoDeleteRemoteSessionIgnoresCanceledParent(t *testing.T) {
	ds := &sessionDeleterStub{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	AutoDeleteRemoteSession(ctx, ds, "single", "acct", "tok", "s1")

	if ds.singleCalls != 1 {
		t.Fatalf("single calls=%d want=1", ds.singleCalls)
	}
	if ds.lastCtxErr != nil {
		t.Fatalf("delete ctx must not inherit parent cancel, got %v", ds.lastCtxErr)
	}
}

func TestAutoDeleteRemoteSessionSwallowsUpstreamError(t *testing.T) {
	ds := &sessionDeleterStub{singleErr: errors.New("upstream 500")}
	// Must not panic and must not propagate the error to callers — the
	// helper is fire-and-forget on the request hot path.
	AutoDeleteRemoteSession(context.Background(), ds, "single", "acct", "tok", "s1")
	if ds.singleCalls != 1 {
		t.Fatalf("expected 1 attempt despite error, got %d", ds.singleCalls)
	}
}

func TestAutoDeleteRemoteSessionNilDeleterIsSafe(t *testing.T) {
	// Defensive: handlers may be wired without a DS in some test fixtures.
	AutoDeleteRemoteSession(context.Background(), nil, "all", "acct", "tok", "s1")
}
