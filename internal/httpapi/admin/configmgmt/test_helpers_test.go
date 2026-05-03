package configmgmt

import (
	"testing"

	"DeepSeek_Web_To_API/internal/account"
	"DeepSeek_Web_To_API/internal/config"
)

func newAdminTestHandler(t *testing.T, raw string) *Handler {
	t.Helper()
	t.Setenv("DEEPSEEK_WEB_TO_API_CONFIG_JSON", raw)
	store := config.LoadStore()
	return &Handler{
		Store: store,
		Pool:  account.NewPool(store),
	}
}
