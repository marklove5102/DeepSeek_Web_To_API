package historycapture

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"DeepSeek_Web_To_API/internal/auth"
	"DeepSeek_Web_To_API/internal/chathistory"
	"DeepSeek_Web_To_API/internal/promptcompat"
)

func TestStartSkipsAdminWebUITesterRequests(t *testing.T) {
	t.Parallel()

	store := chathistory.New(filepath.Join(t.TempDir(), "chat_history.json"))
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Header.Set(AdminWebUISourceHeader, AdminWebUISourceValue)
	session := Start(store, req, &auth.RequestAuth{CallerID: "caller"}, promptcompat.StandardRequest{
		ResponseModel: "claude-sonnet-4-5",
		Messages:      []any{map[string]any{"role": "user", "content": "hello"}},
	})
	if session != nil {
		t.Fatal("expected admin webui tester request to skip history capture")
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if len(snapshot.Items) != 0 {
		t.Fatalf("expected no history items, got %d", len(snapshot.Items))
	}
}

func TestSessionSuccessPersistsConversationDetail(t *testing.T) {
	t.Parallel()

	store := chathistory.New(filepath.Join(t.TempDir(), "chat_history.json"))
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	session := Start(store, req, &auth.RequestAuth{CallerID: "caller", AccountID: "acc-1"}, promptcompat.StandardRequest{
		ResponseModel: "claude-sonnet-4-5",
		Stream:        true,
		Messages:      []any{map[string]any{"role": "user", "content": "hello"}},
		FinalPrompt:   "prompt",
	})
	if session == nil {
		t.Fatal("expected history session")
	}
	session.Success(http.StatusOK, "thinking", "answer", "stop", nil)

	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("expected one history item, got %d", len(snapshot.Items))
	}
	item, err := store.Get(snapshot.Items[0].ID)
	if err != nil {
		t.Fatalf("get item failed: %v", err)
	}
	if item.Status != "success" || item.Content != "answer" || item.ReasoningContent != "thinking" {
		t.Fatalf("unexpected item: %#v", item)
	}
	if item.UserInput != "hello" || item.AccountID != "acc-1" {
		t.Fatalf("unexpected captured request metadata: %#v", item)
	}
}
