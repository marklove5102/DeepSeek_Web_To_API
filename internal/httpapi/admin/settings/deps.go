package settings

import (
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
var intFrom = adminshared.IntFrom

func fieldString(m map[string]any, key string) string {
	return adminshared.FieldString(m, key)
}
func validateRuntimeSettings(runtime config.RuntimeConfig) error {
	return adminshared.ValidateRuntimeSettings(runtime)
}
