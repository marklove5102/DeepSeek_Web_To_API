package history

import (
	"net/http"

	dsclient "DeepSeek_Web_To_API/internal/deepseek/client"
)

func MapError(err error) (int, string) {
	switch {
	case dsclient.IsManagedUnauthorizedError(err):
		return http.StatusUnauthorized, "Account token is invalid. Please re-login the account in admin."
	case dsclient.IsDirectUnauthorizedError(err):
		return http.StatusUnauthorized, "Invalid token. If this should be a DeepSeek_Web_To_API key, add it to config.keys first."
	default:
		return http.StatusInternalServerError, err.Error()
	}
}
