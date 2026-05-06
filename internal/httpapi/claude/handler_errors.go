package claude

import "net/http"

func writeClaudeError(w http.ResponseWriter, status int, message string) {
	writeClaudeErrorWithCode(w, status, message, claudeErrorCode(status))
}

func writeClaudeErrorWithCode(w http.ResponseWriter, status int, message, code string) {
	if code == "" {
		code = claudeErrorCode(status)
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"type":    "invalid_request_error",
			"message": message,
			"code":    code,
			"param":   nil,
		},
	})
}

func claudeErrorCode(status int) string {
	code := "invalid_request"
	switch status {
	case http.StatusUnauthorized:
		code = "authentication_failed"
	case http.StatusTooManyRequests:
		code = "rate_limit_exceeded"
	case http.StatusNotFound:
		code = "not_found"
	case http.StatusBadGateway:
		code = "upstream_error"
	case http.StatusGatewayTimeout:
		code = "upstream_timeout"
	case http.StatusInternalServerError:
		code = "internal_error"
	case 499:
		code = "client_cancelled"
	case 529:
		// Anthropic-defined "overloaded" status. Claude Code's official
		// retry/back-off path keys on this code, so we surface it verbatim
		// instead of falling into the default "invalid_request".
		code = "overloaded_error"
	}
	return code
}
