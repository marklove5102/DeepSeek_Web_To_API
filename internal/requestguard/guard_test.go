package requestguard

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"DeepSeek_Web_To_API/internal/config"
	"DeepSeek_Web_To_API/internal/safetystore"
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

// TestAutoBanTripsAfterRepeatedContentViolations verifies the auto-ban
// behaviour: an IP that fires safety violations repeatedly within the
// sliding window is appended to safety_ips.blocked_ips and the policy
// cache is busted so the very next request is rejected at the IP layer
// rather than at the content scan.
func TestAutoBanTripsAfterRepeatedContentViolations(t *testing.T) {
	store := testStore(t, `{
		"keys":["k"],
		"admin":{"key":"admin","jwt_secret":"secret","jwt_expire_hours":24},
		"safety":{"enabled":true,"banned_content":["blocked phrase"],
		"auto_ban":{"enabled":true,"threshold":3,"window_seconds":600}}
	}`)
	defer func() { _ = store.Close() }()

	ipsPath := filepath.Join(t.TempDir(), "safety_ips.sqlite")
	ipsStore, err := safetystore.NewIPsStore(ipsPath)
	if err != nil {
		t.Fatalf("ips store: %v", err)
	}
	defer func() { _ = ipsStore.Close() }()

	handler := Middleware(Options{Store: store, SafetyIPs: ipsStore})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	body := `{"messages":[{"role":"user","content":"has blocked phrase"}]}`
	mkReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.42:9000"
		return req
	}

	// First three hits are content_blocked (under threshold count and at).
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, mkReq())
		if rec.Code != http.StatusForbidden {
			t.Fatalf("hit %d expected 403, got %d body=%q", i, rec.Code, rec.Body.String())
		}
	}

	// Verify SQLite now lists the IP.
	blocked, _, _, err := ipsStore.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	found := false
	for _, ip := range blocked {
		if ip == "192.0.2.42" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 192.0.2.42 in blocked list, got %#v", blocked)
	}

	// Fourth request: still 403 but now ip_blocked, content scan never runs.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, mkReq())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 after ban, got %d body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "ip_blocked") {
		t.Fatalf("expected ip_blocked code in response, got %q", rec.Body.String())
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
