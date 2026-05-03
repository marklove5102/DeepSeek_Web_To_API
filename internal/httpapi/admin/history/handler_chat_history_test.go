package history

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"DeepSeek_Web_To_API/internal/chathistory"
	"DeepSeek_Web_To_API/internal/config"
)

func newChatHistoryAdminHarness(t *testing.T) (*Handler, *chathistory.Store) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	t.Setenv("DEEPSEEK_WEB_TO_API_CONFIG_PATH", configPath)
	t.Setenv("DEEPSEEK_WEB_TO_API_ADMIN_KEY", "admin")
	t.Setenv("DEEPSEEK_WEB_TO_API_CONFIG_JSON", "")
	store, err := config.LoadStoreWithError()
	if err != nil {
		t.Fatalf("load config store failed: %v", err)
	}
	historyStore := chathistory.New(filepath.Join(dir, "chat_history.json"))
	return &Handler{Store: store, ChatHistory: historyStore}, historyStore
}

func TestGetChatHistoryAndUpdateSettings(t *testing.T) {
	h, historyStore := newChatHistoryAdminHarness(t)
	entry, err := historyStore.Start(chathistory.StartParams{
		CallerID:  "caller:test",
		AccountID: "user@example.com",
		Model:     "deepseek-v4-flash",
		UserInput: "hello",
	})
	if err != nil {
		t.Fatalf("start history failed: %v", err)
	}
	if _, err := historyStore.Update(entry.ID, chathistory.UpdateParams{
		Status:    "success",
		Content:   "world",
		Completed: true,
	}); err != nil {
		t.Fatalf("update history failed: %v", err)
	}

	r := chi.NewRouter()
	RegisterRoutes(r, h)

	req := httptest.NewRequest(http.MethodGet, "/chat-history", nil)
	req.Header.Set("Authorization", "Bearer admin")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload failed: %v", err)
	}
	items, _ := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one history item, got %#v", payload)
	}
	if rec.Header().Get("ETag") == "" {
		t.Fatalf("expected list etag header")
	}

	notModifiedReq := httptest.NewRequest(http.MethodGet, "/chat-history", nil)
	notModifiedReq.Header.Set("Authorization", "Bearer admin")
	notModifiedReq.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	notModifiedRec := httptest.NewRecorder()
	r.ServeHTTP(notModifiedRec, notModifiedReq)
	if notModifiedRec.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d body=%s", notModifiedRec.Code, notModifiedRec.Body.String())
	}

	itemReq := httptest.NewRequest(http.MethodGet, "/chat-history/"+entry.ID, nil)
	itemReq.Header.Set("Authorization", "Bearer admin")
	itemRec := httptest.NewRecorder()
	r.ServeHTTP(itemRec, itemReq)
	if itemRec.Code != http.StatusOK {
		t.Fatalf("expected item 200, got %d body=%s", itemRec.Code, itemRec.Body.String())
	}
	if itemRec.Header().Get("ETag") == "" {
		t.Fatalf("expected detail etag header")
	}

	notModifiedItemReq := httptest.NewRequest(http.MethodGet, "/chat-history/"+entry.ID, nil)
	notModifiedItemReq.Header.Set("Authorization", "Bearer admin")
	notModifiedItemReq.Header.Set("If-None-Match", itemRec.Header().Get("ETag"))
	notModifiedItemRec := httptest.NewRecorder()
	r.ServeHTTP(notModifiedItemRec, notModifiedItemReq)
	if notModifiedItemRec.Code != http.StatusNotModified {
		t.Fatalf("expected detail 304, got %d body=%s", notModifiedItemRec.Code, notModifiedItemRec.Body.String())
	}

	updateReq := httptest.NewRequest(http.MethodPut, "/chat-history/settings", bytes.NewReader([]byte(`{"limit":20000}`)))
	updateReq.Header.Set("Authorization", "Bearer admin")
	updateRec := httptest.NewRecorder()
	r.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from settings update, got %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	snapshot, err := historyStore.Snapshot()
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if snapshot.Limit != chathistory.MaxLimit {
		t.Fatalf("expected limit=%d, got %d", chathistory.MaxLimit, snapshot.Limit)
	}

	disableReq := httptest.NewRequest(http.MethodPut, "/chat-history/settings", bytes.NewReader([]byte(`{"limit":0}`)))
	disableReq.Header.Set("Authorization", "Bearer admin")
	disableRec := httptest.NewRecorder()
	r.ServeHTTP(disableRec, disableReq)
	if disableRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from disable update, got %d body=%s", disableRec.Code, disableRec.Body.String())
	}
	snapshot, err = historyStore.Snapshot()
	if err != nil {
		t.Fatalf("snapshot after disable failed: %v", err)
	}
	if snapshot.Limit != chathistory.DisabledLimit {
		t.Fatalf("expected limit=0, got %d", snapshot.Limit)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("expected history preserved when disabled, got %d", len(snapshot.Items))
	}
}

func TestDeleteAndClearChatHistory(t *testing.T) {
	h, historyStore := newChatHistoryAdminHarness(t)
	entryA, err := historyStore.Start(chathistory.StartParams{UserInput: "a"})
	if err != nil {
		t.Fatalf("start A failed: %v", err)
	}
	if _, err := historyStore.Start(chathistory.StartParams{UserInput: "b"}); err != nil {
		t.Fatalf("start B failed: %v", err)
	}

	r := chi.NewRouter()
	RegisterRoutes(r, h)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/chat-history/"+entryA.ID, nil)
	deleteReq.Header.Set("Authorization", "Bearer admin")
	deleteRec := httptest.NewRecorder()
	r.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected delete 200, got %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}

	snapshot, err := historyStore.Snapshot()
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("expected one item after delete, got %d", len(snapshot.Items))
	}

	clearReq := httptest.NewRequest(http.MethodDelete, "/chat-history", nil)
	clearReq.Header.Set("Authorization", "Bearer admin")
	clearRec := httptest.NewRecorder()
	r.ServeHTTP(clearRec, clearReq)
	if clearRec.Code != http.StatusOK {
		t.Fatalf("expected clear 200, got %d body=%s", clearRec.Code, clearRec.Body.String())
	}

	snapshot, err = historyStore.Snapshot()
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if len(snapshot.Items) != 0 {
		t.Fatalf("expected empty items after clear, got %d", len(snapshot.Items))
	}
}

func TestGetChatHistoryPagination(t *testing.T) {
	h, historyStore := newChatHistoryAdminHarness(t)

	// Create 5 entries with staggered timestamps so ordering is deterministic.
	for i := 0; i < 5; i++ {
		_, err := historyStore.Start(chathistory.StartParams{
			UserInput: "msg",
		})
		if err != nil {
			t.Fatalf("start entry %d failed: %v", i, err)
		}
		// Small sleep to ensure distinct UpdatedAt values.
		time.Sleep(time.Millisecond)
	}

	r := chi.NewRouter()
	RegisterRoutes(r, h)

	// --- default params (no query) should return first page of 100 ---
	defaultReq := httptest.NewRequest(http.MethodGet, "/chat-history", nil)
	defaultReq.Header.Set("Authorization", "Bearer admin")
	defaultRec := httptest.NewRecorder()
	r.ServeHTTP(defaultRec, defaultReq)

	if defaultRec.Code != http.StatusOK {
		t.Fatalf("default page: expected 200, got %d body=%s", defaultRec.Code, defaultRec.Body.String())
	}
	var defaultPayload map[string]any
	if err := json.Unmarshal(defaultRec.Body.Bytes(), &defaultPayload); err != nil {
		t.Fatalf("decode default payload failed: %v", err)
	}
	defaultItems, _ := defaultPayload["items"].([]any)
	if len(defaultItems) != 5 {
		t.Fatalf("default page: expected 5 items, got %d", len(defaultItems))
	}
	if toFloat64(defaultPayload["total"]) != 5 {
		t.Fatalf("default page: expected total=5, got %v", defaultPayload["total"])
	}
	if toFloat64(defaultPayload["count"]) != 5 {
		t.Fatalf("default page: expected count=5, got %v", defaultPayload["count"])
	}

	// --- ?offset=0&limit=2 ---
	page1Req := httptest.NewRequest(http.MethodGet, "/chat-history?offset=0&limit=2", nil)
	page1Req.Header.Set("Authorization", "Bearer admin")
	page1Rec := httptest.NewRecorder()
	r.ServeHTTP(page1Rec, page1Req)

	if page1Rec.Code != http.StatusOK {
		t.Fatalf("page1: expected 200, got %d", page1Rec.Code)
	}
	var page1 map[string]any
	if err := json.Unmarshal(page1Rec.Body.Bytes(), &page1); err != nil {
		t.Fatalf("decode page1 failed: %v", err)
	}
	page1Items, _ := page1["items"].([]any)
	if len(page1Items) != 2 {
		t.Fatalf("page1: expected 2 items, got %d", len(page1Items))
	}
	if toFloat64(page1["total"]) != 5 {
		t.Fatalf("page1: expected total=5, got %v", page1["total"])
	}
	if toFloat64(page1["offset"]) != 0 {
		t.Fatalf("page1: expected offset=0, got %v", page1["offset"])
	}
	if toFloat64(page1["count"]) != 2 {
		t.Fatalf("page1: expected count=2, got %v", page1["count"])
	}

	// --- ?offset=3&limit=2 ---
	page2Req := httptest.NewRequest(http.MethodGet, "/chat-history?offset=3&limit=2", nil)
	page2Req.Header.Set("Authorization", "Bearer admin")
	page2Rec := httptest.NewRecorder()
	r.ServeHTTP(page2Rec, page2Req)

	if page2Rec.Code != http.StatusOK {
		t.Fatalf("page2: expected 200, got %d", page2Rec.Code)
	}
	var page2 map[string]any
	if err := json.Unmarshal(page2Rec.Body.Bytes(), &page2); err != nil {
		t.Fatalf("decode page2 failed: %v", err)
	}
	page2Items, _ := page2["items"].([]any)
	if len(page2Items) != 2 {
		t.Fatalf("page2: expected 2 items, got %d", len(page2Items))
	}
	if toFloat64(page2["total"]) != 5 {
		t.Fatalf("page2: expected total=5, got %v", page2["total"])
	}

	// --- ?offset=10&limit=2 (beyond range) ---
	beyondReq := httptest.NewRequest(http.MethodGet, "/chat-history?offset=10&limit=2", nil)
	beyondReq.Header.Set("Authorization", "Bearer admin")
	beyondRec := httptest.NewRecorder()
	r.ServeHTTP(beyondRec, beyondReq)

	if beyondRec.Code != http.StatusOK {
		t.Fatalf("beyond: expected 200, got %d", beyondRec.Code)
	}
	var beyond map[string]any
	if err := json.Unmarshal(beyondRec.Body.Bytes(), &beyond); err != nil {
		t.Fatalf("decode beyond failed: %v", err)
	}
	beyondItems, _ := beyond["items"].([]any)
	if len(beyondItems) != 0 {
		t.Fatalf("beyond: expected 0 items, got %d", len(beyondItems))
	}
	if toFloat64(beyond["total"]) != 5 {
		t.Fatalf("beyond: expected total=5, got %v", beyond["total"])
	}

	// --- ETag still works with pagination ---
	etag1 := page1Rec.Header().Get("ETag")
	if etag1 == "" {
		t.Fatalf("expected ETag header on paginated response")
	}
	etagReq := httptest.NewRequest(http.MethodGet, "/chat-history?offset=0&limit=2", nil)
	etagReq.Header.Set("Authorization", "Bearer admin")
	etagReq.Header.Set("If-None-Match", etag1)
	etagRec := httptest.NewRecorder()
	r.ServeHTTP(etagRec, etagReq)
	if etagRec.Code != http.StatusNotModified {
		t.Fatalf("ETag: expected 304, got %d body=%s", etagRec.Code, etagRec.Body.String())
	}

	// --- Different pagination params produce different ETags ---
	etag2 := page2Rec.Header().Get("ETag")
	if etag2 == "" {
		t.Fatalf("expected ETag header on page2 response")
	}
	if etag1 == etag2 {
		t.Fatalf("ETag: page1 and page2 must have different ETags, got %s", etag1)
	}

	// --- Cross-page ETag must NOT cause false 304 ---
	crossReq := httptest.NewRequest(http.MethodGet, "/chat-history?offset=0&limit=2", nil)
	crossReq.Header.Set("Authorization", "Bearer admin")
	crossReq.Header.Set("If-None-Match", etag2)
	crossRec := httptest.NewRecorder()
	r.ServeHTTP(crossRec, crossReq)
	if crossRec.Code == http.StatusNotModified {
		t.Fatalf("ETag cross-page: page2 ETag must not produce 304 for page1 request")
	}
	if crossRec.Code != http.StatusOK {
		t.Fatalf("ETag cross-page: expected 200, got %d body=%s", crossRec.Code, crossRec.Body.String())
	}
}

func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return -1
	}
}
