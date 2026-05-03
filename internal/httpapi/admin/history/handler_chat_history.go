package history

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"DeepSeek_Web_To_API/internal/chathistory"
)

func (h *Handler) getChatHistory(w http.ResponseWriter, r *http.Request) {
	store := h.ChatHistory
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "chat history store is not configured"})
		return
	}
	offsetRaw := r.URL.Query().Get("offset")
	offset, err := strconv.Atoi(offsetRaw)
	if err != nil && offsetRaw != "" {
		// #nosec G706 -- query text is stripped of control characters and length-capped.
		slog.Warn("chat-history: invalid offset query param", "value", sanitizeLogValue(offsetRaw), "error", err)
	}
	if offset < 0 {
		offset = 0
	}
	limitRaw := r.URL.Query().Get("limit")
	pageLimit, err := strconv.Atoi(limitRaw)
	if err != nil && limitRaw != "" {
		// #nosec G706 -- query text is stripped of control characters and length-capped.
		slog.Warn("chat-history: invalid limit query param", "value", sanitizeLogValue(limitRaw), "error", err)
	}
	if pageLimit <= 0 || pageLimit > 500 {
		pageLimit = 100
	}
	ifNoneMatch := strings.TrimSpace(r.Header.Get("If-None-Match"))
	if ifNoneMatch != "" {
		revision, err := store.Revision()
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"detail": err.Error(),
				"path":   store.Path(),
			})
			return
		}
		etag := chathistory.ListETag(revision, offset, pageLimit)
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-cache")
		if ifNoneMatch == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	snapshot, total, err := store.SnapshotPage(offset, pageLimit)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"detail": err.Error(),
			"path":   store.Path(),
		})
		return
	}
	etag := chathistory.ListETag(snapshot.Revision, offset, pageLimit)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if ifNoneMatch == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":  snapshot.Version,
		"limit":    snapshot.Limit,
		"revision": snapshot.Revision,
		"total":    total,
		"offset":   offset,
		"count":    len(snapshot.Items),
		"items":    snapshot.Items,
		"path":     store.Path(),
	})
}

func sanitizeLogValue(raw string) string {
	raw = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, raw)
	runes := []rune(raw)
	if len(runes) > 128 {
		return string(runes[:128]) + "..."
	}
	return raw
}

func (h *Handler) getChatHistoryItem(w http.ResponseWriter, r *http.Request) {
	store := h.ChatHistory
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "chat history store is not configured"})
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "history id is required"})
		return
	}
	ifNoneMatch := strings.TrimSpace(r.Header.Get("If-None-Match"))
	if ifNoneMatch != "" {
		revision, err := store.DetailRevision(id)
		if err != nil {
			status := http.StatusInternalServerError
			if strings.Contains(strings.ToLower(err.Error()), "not found") {
				status = http.StatusNotFound
			}
			writeJSON(w, status, map[string]any{"detail": err.Error()})
			return
		}
		etag := chathistory.DetailETag(id, revision)
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-cache")
		if ifNoneMatch == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	item, err := store.Get(id)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"detail": err.Error()})
		return
	}
	etag := chathistory.DetailETag(item.ID, item.Revision)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if ifNoneMatch == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"item": item,
	})
}

func (h *Handler) clearChatHistory(w http.ResponseWriter, _ *http.Request) {
	store := h.ChatHistory
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "chat history store is not configured"})
		return
	}
	if err := store.Clear(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": err.Error(), "path": store.Path()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *Handler) deleteChatHistoryItem(w http.ResponseWriter, r *http.Request) {
	store := h.ChatHistory
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "chat history store is not configured"})
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "history id is required"})
		return
	}
	if err := store.Delete(id); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *Handler) updateChatHistorySettings(w http.ResponseWriter, r *http.Request) {
	store := h.ChatHistory
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "chat history store is not configured"})
		return
	}
	var body struct {
		Limit int `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid json"})
		return
	}
	snapshot, err := store.SetLimit(body.Limit)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"limit":    snapshot.Limit,
		"revision": snapshot.Revision,
		"items":    snapshot.Items,
	})
}
