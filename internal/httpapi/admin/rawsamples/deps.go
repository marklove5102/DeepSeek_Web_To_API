package rawsamples

import (
	"net/http"

	"DeepSeek_Web_To_API/internal/chathistory"
	"DeepSeek_Web_To_API/internal/config"
	adminshared "DeepSeek_Web_To_API/internal/httpapi/admin/shared"
)

type Handler struct {
	Store       adminshared.ConfigStore
	Pool        adminshared.PoolController
	DS          adminshared.DeepSeekCaller
	OpenAI      adminshared.OpenAIChatCaller
	ChatHistory *chathistory.Store
}

var writeJSON = adminshared.WriteJSON

func (h *Handler) rawStreamSampleRoot() string {
	if h != nil && h.Store != nil {
		return h.Store.RawStreamSampleRoot()
	}
	return config.RawStreamSampleRoot()
}

func intFromQuery(r *http.Request, key string, d int) int {
	return adminshared.IntFromQuery(r, key, d)
}
func nilIfEmpty(s string) any              { return adminshared.NilIfEmpty(s) }
func toStringSlice(v any) ([]string, bool) { return adminshared.ToStringSlice(v) }
func fieldString(m map[string]any, key string) string {
	return adminshared.FieldString(m, key)
}
