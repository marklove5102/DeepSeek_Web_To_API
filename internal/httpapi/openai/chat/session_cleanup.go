package chat

import (
	"context"

	"DeepSeek_Web_To_API/internal/auth"
	"DeepSeek_Web_To_API/internal/httpapi/openai/shared"
)

// autoDeleteRemoteSession is a thin wrapper that snapshots the operator's
// auto-delete mode from the Store and forwards to the shared cleanup
// implementation. The shared implementation (Issue #20 fix) is now reused
// from /v1/responses and /v1/messages so all three paths honor the same
// WebUI toggle.
func (h *Handler) autoDeleteRemoteSession(ctx context.Context, a *auth.RequestAuth, sessionID string) {
	if h == nil || h.Store == nil || a == nil {
		return
	}
	shared.AutoDeleteRemoteSession(ctx, h.DS, h.Store.AutoDeleteMode(), a.AccountID, a.DeepSeekToken, sessionID)
}
