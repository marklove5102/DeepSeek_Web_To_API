package chat

import (
	"net/http"

	"DeepSeek_Web_To_API/internal/httpapi/openai/shared"
)

func writeCompletionCallError(w http.ResponseWriter, historySession *chatHistorySession, err error, thinking, content string) {
	detail := shared.CompletionErrorDetail(err)
	writeUpstreamCallError(w, historySession, detail, thinking, content)
}

func writeSessionCallError(w http.ResponseWriter, historySession *chatHistorySession, err error) {
	detail := shared.SessionErrorDetail(err)
	writeUpstreamCallError(w, historySession, detail, "", "")
}

func writePowCallError(w http.ResponseWriter, historySession *chatHistorySession, err error) {
	detail := shared.PowErrorDetail(err)
	writeUpstreamCallError(w, historySession, detail, "", "")
}

func writeUpstreamCallError(w http.ResponseWriter, historySession *chatHistorySession, detail shared.UpstreamErrorDetail, thinking, content string) {
	if historySession != nil {
		if detail.Stopped {
			historySession.stopped(thinking, content, detail.FinishReason)
		} else {
			historySession.error(detail.Status, detail.Message, detail.FinishReason, thinking, content)
		}
	}
	writeOpenAIErrorWithCode(w, detail.Status, detail.Message, detail.Code)
}

func failChatStreamCompletionError(streamRuntime *chatStreamRuntime, historySession *chatHistorySession, err error) {
	detail := shared.CompletionErrorDetail(err)
	if detail.Stopped {
		if historySession != nil {
			historySession.stopped(streamRuntime.thinking.String(), streamRuntime.text.String(), detail.FinishReason)
		}
		return
	}
	failChatStreamRetry(streamRuntime, historySession, detail.Status, detail.Message, detail.Code)
}
