package requestmeta

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConversationIDUsesToolchainHeaders(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("X-Codex-Session-ID", "codex-session-1")

	if got := ConversationID(req, nil); got != "codex-session-1" {
		t.Fatalf("conversation id=%q", got)
	}
}

func TestConversationIDFallsBackToBodyMetadata(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := []byte(`{"metadata":{"conversation_id":"conv-body-1"},"input":"hello"}`)

	if got := ConversationID(req, body); got != "conv-body-1" {
		t.Fatalf("conversation id=%q", got)
	}
}

func TestClientIPNormalizesRemoteAddr(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.RemoteAddr = "203.0.113.10:4567"

	if got := ClientIP(req); got != "203.0.113.10" {
		t.Fatalf("client ip=%q", got)
	}
}

func TestClientIPPrefersForwardedHeader(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.RemoteAddr = "127.0.0.1:4567"
	req.Header.Set("X-Forwarded-For", "198.51.100.11, 10.0.0.1")

	if got := ClientIP(req); got != "198.51.100.11" {
		t.Fatalf("client ip=%q", got)
	}
}
