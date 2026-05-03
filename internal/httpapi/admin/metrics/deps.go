package metrics

import (
	"DeepSeek_Web_To_API/internal/chathistory"
	adminshared "DeepSeek_Web_To_API/internal/httpapi/admin/shared"
)

type Handler struct {
	Store         adminshared.ConfigStore
	Pool          adminshared.PoolController
	ChatHistory   *chathistory.Store
	ResponseCache adminshared.ResponseCacheStatsProvider
}

var writeJSON = adminshared.WriteJSON
