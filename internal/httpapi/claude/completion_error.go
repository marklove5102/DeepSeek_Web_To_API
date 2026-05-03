package claude

import (
	"net/http"

	"DeepSeek_Web_To_API/internal/httpapi/historycapture"
	openaishared "DeepSeek_Web_To_API/internal/httpapi/openai/shared"
)

func writeClaudeCompletionCallError(w http.ResponseWriter, historySession *historycapture.Session, err error, thinking, content string) {
	detail := openaishared.CompletionErrorDetail(err)
	if historySession != nil {
		if detail.Stopped {
			historySession.Stopped(thinking, content, detail.FinishReason)
		} else {
			historySession.Error(detail.Status, detail.Message, detail.FinishReason, thinking, content)
		}
	}
	writeClaudeErrorWithCode(w, detail.Status, detail.Message, detail.Code)
}

func writeClaudeSessionCallError(w http.ResponseWriter, historySession *historycapture.Session, err error) {
	detail := openaishared.SessionErrorDetail(err)
	if historySession != nil {
		if detail.Stopped {
			historySession.Stopped("", "", detail.FinishReason)
		} else {
			historySession.Error(detail.Status, detail.Message, detail.FinishReason, "", "")
		}
	}
	writeClaudeErrorWithCode(w, detail.Status, detail.Message, detail.Code)
}

func writeClaudePowCallError(w http.ResponseWriter, historySession *historycapture.Session, err error) {
	detail := openaishared.PowErrorDetail(err)
	if historySession != nil {
		if detail.Stopped {
			historySession.Stopped("", "", detail.FinishReason)
		} else {
			historySession.Error(detail.Status, detail.Message, detail.FinishReason, "", "")
		}
	}
	writeClaudeErrorWithCode(w, detail.Status, detail.Message, detail.Code)
}
