package history

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"DeepSeek_Web_To_API/internal/auth"
	"DeepSeek_Web_To_API/internal/config"
	dsclient "DeepSeek_Web_To_API/internal/deepseek/client"
	"DeepSeek_Web_To_API/internal/httpapi/openai/shared"
	"DeepSeek_Web_To_API/internal/promptcompat"
	"DeepSeek_Web_To_API/internal/util"
)

const (
	currentInputFilename    = promptcompat.CurrentInputContextFilename
	currentInputContentType = "text/plain; charset=utf-8"
	currentInputPurpose     = "assistants"
)

func (s Service) ApplyCurrentInputFile(ctx context.Context, a *auth.RequestAuth, stdReq promptcompat.StandardRequest) (promptcompat.StandardRequest, error) {
	if s.DS == nil || s.Store == nil || a == nil || !s.Store.CurrentInputFileEnabled() {
		return stdReq, nil
	}
	threshold := s.Store.CurrentInputFileMinChars()

	index, text := latestUserInputForFile(stdReq.Messages)
	if index < 0 {
		return stdReq, nil
	}
	if len([]rune(text)) < threshold {
		return stdReq, nil
	}
	fileText := promptcompat.BuildOpenAICurrentInputContextTranscript(stdReq.Messages)
	if strings.TrimSpace(fileText) == "" {
		return stdReq, errors.New("current user input file produced empty transcript")
	}

	if !s.Store.RemoteFileUploadEnabled() {
		// Inline the transcript directly into the conversation rather than
		// uploading it to DeepSeek's file API. The upstream upload_file
		// endpoint is heavily rate-limited per account ("rate limit
		// reached"), and on busy multi-turn workloads this single feature
		// was the dominant cause of the production failure rate. We now
		// feed the full transcript as the user message body so the model
		// still sees the entire context window without hitting upload_file
		// at all. Operators can opt back into uploading via env
		// DEEPSEEK_WEB_TO_API_REMOTE_FILE_UPLOAD_ENABLED=true.
		inlinedContent := fileText + "\n\n---\n\n" + currentInputFileInlinePrompt()
		messages := []any{
			map[string]any{
				"role":    "user",
				"content": inlinedContent,
			},
		}
		stdReq.Messages = messages
		stdReq.HistoryText = fileText
		stdReq.CurrentInputFileApplied = true
		stdReq.FinalPrompt, stdReq.ToolNames = promptcompat.BuildOpenAIPrompt(messages, stdReq.ToolsRaw, "", stdReq.ToolChoice, stdReq.Thinking)
		stdReq.RefFileTokens += util.CountPromptTokens(fileText, stdReq.ResponseModel)
		stdReq.PromptTokenText = stdReq.FinalPrompt
		return stdReq, nil
	}

	modelType := "default"
	if resolvedType, ok := config.GetModelType(stdReq.ResolvedModel); ok {
		modelType = resolvedType
	}
	result, err := s.DS.UploadFile(ctx, a, dsclient.UploadFileRequest{
		Filename:    currentInputFilename,
		ContentType: currentInputContentType,
		Purpose:     currentInputPurpose,
		ModelType:   modelType,
		Data:        []byte(fileText),
	}, 3)
	if err != nil {
		return stdReq, fmt.Errorf("upload current user input file: %w", err)
	}
	fileID := strings.TrimSpace(result.ID)
	if fileID == "" {
		return stdReq, errors.New("upload current user input file returned empty file id")
	}

	messages := []any{
		map[string]any{
			"role":    "user",
			"content": currentInputFilePrompt(),
		},
	}

	stdReq.Messages = messages
	stdReq.HistoryText = fileText
	stdReq.CurrentInputFileApplied = true
	stdReq.RefFileIDs = prependUniqueRefFileID(stdReq.RefFileIDs, fileID)
	stdReq.FinalPrompt, stdReq.ToolNames = promptcompat.BuildOpenAIPrompt(messages, stdReq.ToolsRaw, "", stdReq.ToolChoice, stdReq.Thinking)
	stdReq.RefFileTokens += util.CountPromptTokens(fileText, stdReq.ResponseModel)
	stdReq.PromptTokenText = fileText + "\n" + stdReq.FinalPrompt
	return stdReq, nil
}

func latestUserInputForFile(messages []any) (int, string) {
	for i := len(messages) - 1; i >= 0; i-- {
		msg, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(shared.AsString(msg["role"])))
		if role != "user" {
			continue
		}
		text := promptcompat.NormalizeOpenAIContentForPrompt(msg["content"])
		if strings.TrimSpace(text) == "" {
			return -1, ""
		}
		return i, text
	}
	return -1, ""
}

func currentInputFilePrompt() string {
	return "Continue from the latest state in the attached DEEPSEEK_WEB_TO_API_HISTORY.txt context. Treat it as the current working state and answer the latest user request directly."
}

func currentInputFileInlinePrompt() string {
	return "Treat everything above as the prior conversation transcript and respond to the latest user request directly."
}
