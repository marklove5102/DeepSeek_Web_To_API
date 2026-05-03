package version

import (
	"net/http"
	"time"

	"DeepSeek_Web_To_API/internal/version"
)

func (h *Handler) getVersion(w http.ResponseWriter, _ *http.Request) {
	current, source := version.Current()
	resp := map[string]any{
		"success":         true,
		"current_version": current,
		"current_tag":     version.Tag(current),
		"source":          source,
		"update_policy":   "self_managed",
		"checked_at":      time.Now().UTC().Format(time.RFC3339),
	}

	writeJSON(w, http.StatusOK, resp)
}
