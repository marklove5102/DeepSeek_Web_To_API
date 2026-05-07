package shared

import (
	"context"
	"net/http"

	"DeepSeek_Web_To_API/internal/auth"
	"DeepSeek_Web_To_API/internal/safetyllm"
)

// PickAuditText picks the best string to feed the safety LLM: the
// latest user message text if available, otherwise falls back to the
// fully-built final prompt (legacy behavior). v1.0.19+: callers should
// pass stdReq.LatestUserText first; if empty (extractor couldn't find
// a user message) the final prompt is the safe fallback.
func PickAuditText(latestUserText, finalPrompt string) string {
	if latestUserText != "" {
		return latestUserText
	}
	return finalPrompt
}

// RunSafetyCheckAndBlock is the shared "run safety check, block if
// violation" helper used by /v1/chat/completions, /v1/responses, and
// /v1/messages handlers. Returns true when the caller must halt
// (response already written) or false when the request should proceed
// upstream.
//
// nil checker / Enabled()==false → no-op (returns false, proceed).
//
// Block path: write 403 with the operator-configured block_message
// (falls back to a sane default), set finish_reason via
// historySession.Error so chat_history records it as policy_blocked
// (FailureRateExcludedStatusCodes ignores 403 → success-rate metric
// stays clean).
func RunSafetyCheckAndBlock(ctx context.Context, checker safetyllm.Checker, a *auth.RequestAuth, text string, w http.ResponseWriter, blockMessage string, onBlock func(verdict safetyllm.Verdict)) bool {
	if checker == nil || !checker.Enabled() {
		return false
	}
	// safetyllm itself never returns err in fail-open; a non-nil err is a
	// fail-closed verdict already shaped as Violation=true, so we discard
	// the error and read the verdict directly.
	verdict, _ := checker.CheckWithAuth(ctx, a, text)
	if !verdict.Violation {
		return false
	}
	if onBlock != nil {
		onBlock(verdict)
	}
	if blockMessage == "" {
		blockMessage = "该请求触发了内容安全策略，已被拒绝。"
	}
	WriteJSON(w, http.StatusForbidden, map[string]any{
		"error": map[string]any{
			"message": blockMessage,
			"type":    "policy_blocked",
			"code":    "llm_safety_blocked",
		},
	})
	return true
}
