package requestguard

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"DeepSeek_Web_To_API/internal/config"
)

func TestMiddlewareBlocksConfiguredContent(t *testing.T) {
	store := testStore(t, `{
		"keys":["k"],
		"admin":{"key":"admin","jwt_secret":"secret","jwt_expire_hours":24},
		"safety":{"enabled":true,"banned_content":["blocked phrase"]}
	}`)
	defer func() { _ = store.Close() }()
	handler := Middleware(Options{Store: store})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"has blocked phrase"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "content_blocked") {
		t.Fatalf("expected content block code, got %q", rec.Body.String())
	}
}

func TestMiddlewareAddsRequestMetadata(t *testing.T) {
	store := testStore(t, `{
		"keys":["k"],
		"admin":{"key":"admin","jwt_secret":"secret","jwt_expire_hours":24}
	}`)
	defer func() { _ = store.Close() }()
	var seenIP, seenConversation string
	handler := Middleware(Options{Store: store})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meta, ok := FromContext(r.Context())
		if !ok {
			t.Fatal("missing request metadata")
		}
		seenIP = meta.ClientIP
		seenConversation = meta.ConversationID
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"ok"}]}`))
	req.RemoteAddr = "198.51.100.8:1234"
	req.Header.Set("X-OpenCode-Session-ID", "opencode-1")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%q", rec.Code, rec.Body.String())
	}
	if seenIP != "198.51.100.8" || seenConversation != "opencode-1" {
		t.Fatalf("metadata ip=%q conversation=%q", seenIP, seenConversation)
	}
}

func testStore(t *testing.T, cfg string) *config.Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DEEPSEEK_WEB_TO_API_CONFIG_PATH", path)
	t.Setenv("DEEPSEEK_WEB_TO_API_ACCOUNTS_SQLITE_PATH", filepath.Join(dir, "accounts.sqlite"))
	store, err := config.LoadStoreWithError()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return store
}
