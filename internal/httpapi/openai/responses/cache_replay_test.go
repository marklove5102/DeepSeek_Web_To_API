package responses

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"DeepSeek_Web_To_API/internal/responsecache"
)

func TestOnProtocolResponseCacheHitStoresNonStreamResponse(t *testing.T) {
	store, resolver := newDirectTokenResolver(t)
	h := &Handler{Store: store, Auth: resolver}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req.Header.Set("Authorization", "Bearer token-a")

	h.OnProtocolResponseCacheHit(req, responsecache.Entry{
		Status: http.StatusOK,
		Body:   []byte(`{"id":"resp_cached","object":"response","status":"completed"}`),
	}, "memory")

	owner := responseStoreOwner(authForToken(t, resolver, "token-a"))
	got, ok := h.getResponseStore().get(owner, "resp_cached")
	if !ok {
		t.Fatal("expected cached response to be stored")
	}
	if got["status"] != "completed" {
		t.Fatalf("unexpected stored response: %#v", got)
	}
}

func TestOnProtocolResponseCacheHitStoresStreamCompletedResponse(t *testing.T) {
	store, resolver := newDirectTokenResolver(t)
	h := &Handler{Store: store, Auth: resolver}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req.Header.Set("Authorization", "Bearer token-a")
	body := []byte("event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_stream_cached\",\"object\":\"response\",\"status\":\"completed\"}}\n\n" +
		"data: [DONE]\n\n")

	h.OnProtocolResponseCacheHit(req, responsecache.Entry{
		Status: http.StatusOK,
		Body:   body,
	}, "disk")

	owner := responseStoreOwner(authForToken(t, resolver, "token-a"))
	if _, ok := h.getResponseStore().get(owner, "resp_stream_cached"); !ok {
		t.Fatal("expected cached stream response to be stored")
	}
}

func TestOnProtocolResponseCacheHitIgnoresOtherPaths(t *testing.T) {
	store, resolver := newDirectTokenResolver(t)
	h := &Handler{Store: store, Auth: resolver}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer token-a")

	h.OnProtocolResponseCacheHit(req, responsecache.Entry{
		Status: http.StatusOK,
		Body:   []byte(`{"id":"resp_cached","object":"response"}`),
	}, "memory")

	owner := responseStoreOwner(authForToken(t, resolver, "token-a"))
	if _, ok := h.getResponseStore().get(owner, "resp_cached"); ok {
		t.Fatal("expected non-responses path to be ignored")
	}
}
