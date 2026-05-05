package settings

import (
	"net/http"
	"strings"

	authn "DeepSeek_Web_To_API/internal/auth"
	"DeepSeek_Web_To_API/internal/promptcompat"
)

func (h *Handler) getSettings(w http.ResponseWriter, _ *http.Request) {
	snap := h.Store.Snapshot()
	recommended := defaultRuntimeRecommended(len(snap.Accounts), h.Store.RuntimeAccountMaxInflight())
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"admin": map[string]any{
			"has_password_hash":        strings.TrimSpace(snap.Admin.PasswordHash) != "",
			"jwt_expire_hours":         h.Store.AdminJWTExpireHours(),
			"jwt_valid_after_unix":     snap.Admin.JWTValidAfterUnix,
			"default_password_warning": authn.UsingDefaultAdminKey(h.Store),
		},
		"runtime": map[string]any{
			"account_max_inflight":         h.Store.RuntimeAccountMaxInflight(),
			"account_max_queue":            h.Store.RuntimeAccountMaxQueue(recommended),
			"global_max_inflight":          h.Store.RuntimeGlobalMaxInflight(recommended),
			"token_refresh_interval_hours": h.Store.RuntimeTokenRefreshIntervalHours(),
		},
		"compat": map[string]any{
			"wide_input_strict_output": h.Store.CompatWideInputStrictOutput(),
			"strip_reference_markers":  h.Store.CompatStripReferenceMarkers(),
		},
		"responses": map[string]any{
			"store_ttl_seconds": h.Store.ResponsesStoreTTLSeconds(),
		},
		"embeddings": map[string]any{
			"provider": h.Store.EmbeddingsProvider(),
		},
		"cache":       h.responseCacheSettings(),
		"safety":      snap.Safety,
		"auto_delete": snap.AutoDelete,
		"current_input_file": map[string]any{
			"enabled":   h.Store.CurrentInputFileEnabled(),
			"min_chars": h.Store.CurrentInputFileMinChars(),
		},
		"thinking_injection": map[string]any{
			"enabled":        h.Store.ThinkingInjectionEnabled(),
			"prompt":         h.Store.ThinkingInjectionPrompt(),
			"default_prompt": promptcompat.DefaultThinkingInjectionPrompt,
		},
		"model_aliases": snap.ModelAliases,
		"env_backed":    h.Store.IsEnvBacked(),
	})
}

func (h *Handler) responseCacheSettings() map[string]any {
	response := map[string]any{
		"dir":                h.Store.ResponseCacheDir(),
		"memory_ttl_seconds": int(h.Store.ResponseCacheMemoryTTL().Seconds()),
		"memory_max_bytes":   h.Store.ResponseCacheMemoryMaxBytes(),
		"disk_ttl_seconds":   int(h.Store.ResponseCacheDiskTTL().Seconds()),
		"disk_max_bytes":     h.Store.ResponseCacheDiskMaxBytes(),
		"max_body_bytes":     h.Store.ResponseCacheMaxBodyBytes(),
		"semantic_key":       h.Store.ResponseCacheSemanticKey(),
		"compression":        "gzip",
	}
	if h.ResponseCache != nil {
		stats := h.ResponseCache.Stats()
		for _, key := range []string{
			"disk_dir",
			"memory_ttl_seconds",
			"memory_max_bytes",
			"disk_ttl_seconds",
			"disk_max_bytes",
			"max_body_bytes",
			"semantic_key",
			"compression",
		} {
			if value, ok := stats[key]; ok {
				if key == "disk_dir" {
					response["dir"] = value
				} else {
					response[key] = value
				}
			}
		}
	}
	return map[string]any{"response": response}
}
