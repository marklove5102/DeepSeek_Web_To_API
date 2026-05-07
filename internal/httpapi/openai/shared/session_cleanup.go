package shared

import (
	"context"
	"time"

	"DeepSeek_Web_To_API/internal/config"
	dsclient "DeepSeek_Web_To_API/internal/deepseek/client"
)

// SessionDeleter is the minimal upstream-client surface AutoDeleteRemoteSession
// needs. dsclient.Client satisfies it; tests use stubs.
type SessionDeleter interface {
	DeleteSessionForToken(ctx context.Context, token string, sessionID string) (*dsclient.DeleteSessionResult, error)
	DeleteAllSessionsForToken(ctx context.Context, token string) error
}

// AutoDeleteRemoteSession asks DeepSeek to drop the session(s) created for
// this request, honoring the operator's per-completion auto-delete setting.
// Modes: "none" (no-op), "single" (delete just sessionID), "all" (wipe every
// session on the account).
//
// Issue #20 fix: this used to live in internal/httpapi/openai/chat as an
// unexported method, so only /v1/chat/completions ever cleaned up. The
// /v1/responses (OpenAI Responses) and /v1/messages (Anthropic / Claude
// Code) handlers had no equivalent path, leaving the operator's WebUI
// "auto-delete" toggle a silent no-op for those clients.
//
// The function decouples the cancel-state of the HTTP request context from
// the upstream delete via context.WithoutCancel — by the time the chat
// handler's defer fires, the request context is already cancelled, but the
// 10-second timeout box gives the delete a bounded window to land.
func AutoDeleteRemoteSession(ctx context.Context, ds SessionDeleter, mode, accountID, deepseekToken, sessionID string) {
	if ds == nil {
		return
	}
	if mode == "" || mode == "none" {
		return
	}
	if deepseekToken == "" {
		return
	}

	deleteBaseCtx := context.WithoutCancel(ctx)
	deleteCtx, cancel := context.WithTimeout(deleteBaseCtx, 10*time.Second)
	defer cancel()

	switch mode {
	case "single":
		if sessionID == "" {
			config.Logger.Warn("[auto_delete_sessions] skipped single-session delete because session_id is empty", "account", accountID)
			return
		}
		if _, err := ds.DeleteSessionForToken(deleteCtx, deepseekToken, sessionID); err != nil {
			config.Logger.Warn("[auto_delete_sessions] failed", "account", accountID, "mode", mode, "session_id", sessionID, "error", err)
			return
		}
		config.Logger.Debug("[auto_delete_sessions] success", "account", accountID, "mode", mode, "session_id", sessionID)
	case "all":
		if err := ds.DeleteAllSessionsForToken(deleteCtx, deepseekToken); err != nil {
			config.Logger.Warn("[auto_delete_sessions] failed", "account", accountID, "mode", mode, "error", err)
			return
		}
		config.Logger.Debug("[auto_delete_sessions] success", "account", accountID, "mode", mode)
	default:
		config.Logger.Warn("[auto_delete_sessions] unknown mode", "account", accountID, "mode", mode)
	}
}
