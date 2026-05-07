package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"DeepSeek_Web_To_API/internal/auth"
	dsclient "DeepSeek_Web_To_API/internal/deepseek/client"
	openaishared "DeepSeek_Web_To_API/internal/httpapi/openai/shared"
	"DeepSeek_Web_To_API/internal/promptcompat"
	"DeepSeek_Web_To_API/internal/util"
)

func historySplitTestMessages() []any {
	toolCalls := []any{
		map[string]any{
			"name":      "search",
			"arguments": map[string]any{"query": "docs"},
		},
	}
	return []any{
		map[string]any{"role": "system", "content": "system instructions"},
		map[string]any{"role": "user", "content": "first user turn"},
		map[string]any{
			"role":              "assistant",
			"content":           "",
			"reasoning_content": "hidden reasoning",
			"tool_calls":        toolCalls,
		},
		map[string]any{
			"role":         "tool",
			"name":         "search",
			"tool_call_id": "call-1",
			"content":      "tool result",
		},
		map[string]any{"role": "user", "content": "latest user turn"},
	}
}

type streamStatusManagedAuthStub struct{}

func (streamStatusManagedAuthStub) Determine(_ *http.Request) (*auth.RequestAuth, error) {
	return &auth.RequestAuth{
		UseConfigToken: true,
		DeepSeekToken:  "managed-token",
		CallerID:       "caller:test",
		AccountID:      "acct:test",
		TriedAccounts:  map[string]bool{},
	}, nil
}

func (streamStatusManagedAuthStub) DetermineCaller(_ *http.Request) (*auth.RequestAuth, error) {
	return (&streamStatusManagedAuthStub{}).Determine(nil)
}

func (s streamStatusManagedAuthStub) DetermineWithSession(req *http.Request, _ []byte) (*auth.RequestAuth, error) {
	return s.Determine(req)
}

func (streamStatusManagedAuthStub) Release(_ *auth.RequestAuth) {}

func TestBuildOpenAICurrentInputContextTranscriptUsesPlainTranscript(t *testing.T) {
	_, historyMessages := splitOpenAIHistoryMessages(historySplitTestMessages(), 1)
	transcript := buildOpenAICurrentInputContextTranscript(historyMessages)

	if strings.Contains(transcript, "[file content end]") || strings.Contains(transcript, "[file name]:") || strings.Contains(transcript, "[file content begin]") {
		t.Fatalf("expected plain transcript without injected file wrapper, got %q", transcript)
	}
	if !strings.Contains(transcript, "# DEEPSEEK_WEB_TO_API_HISTORY.txt") {
		t.Fatalf("expected history transcript header, got %q", transcript)
	}
	if !strings.Contains(transcript, "Prior conversation history and tool progress.") {
		t.Fatalf("expected history transcript description, got %q", transcript)
	}
	if !strings.Contains(transcript, "[reasoning_content]") || !strings.Contains(transcript, "hidden reasoning") {
		t.Fatalf("expected reasoning block preserved, got %q", transcript)
	}
	if !strings.Contains(transcript, "<|DSML|tool_calls>") {
		t.Fatalf("expected tool calls preserved, got %q", transcript)
	}
}

func TestSplitOpenAIHistoryMessagesUsesLatestUserTurn(t *testing.T) {
	messages := []any{
		map[string]any{"role": "system", "content": "system instructions"},
		map[string]any{"role": "user", "content": "first user turn"},
		map[string]any{"role": "assistant", "content": "first assistant turn"},
		map[string]any{"role": "user", "content": "middle user turn"},
		map[string]any{"role": "assistant", "content": "middle assistant turn"},
		map[string]any{"role": "user", "content": "latest user turn"},
	}

	promptMessages, historyMessages := splitOpenAIHistoryMessages(messages, 1)
	if len(promptMessages) == 0 || len(historyMessages) == 0 {
		t.Fatalf("expected both prompt and history messages, got prompt=%d history=%d", len(promptMessages), len(historyMessages))
	}

	promptText, _ := promptcompat.BuildOpenAIPrompt(promptMessages, nil, "", defaultToolChoicePolicy(), true)
	if !strings.Contains(promptText, "latest user turn") {
		t.Fatalf("expected latest user turn in prompt, got %s", promptText)
	}
	if strings.Contains(promptText, "middle user turn") {
		t.Fatalf("expected middle user turn to be moved into history, got %s", promptText)
	}

	historyText := buildOpenAICurrentInputContextTranscript(historyMessages)
	if !strings.Contains(historyText, "middle user turn") {
		t.Fatalf("expected middle user turn in split history, got %s", historyText)
	}
	if strings.Contains(historyText, "latest user turn") {
		t.Fatalf("expected latest user turn to remain live, got %s", historyText)
	}
}

func TestApplyCurrentInputFileSkipsShortInputWhenThresholdNotReached(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			currentInputEnabled: true,
			currentInputMin:     10,
		},
		DS: ds,
	}
	req := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	stdReq, err := promptcompat.NormalizeOpenAIChatRequest(h.Store, req, "")
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	out, err := h.applyCurrentInputFile(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, stdReq)
	if err != nil {
		t.Fatalf("apply current input file failed: %v", err)
	}
	if len(ds.uploadCalls) != 0 {
		t.Fatalf("expected no upload on first turn, got %d", len(ds.uploadCalls))
	}
	if out.FinalPrompt != stdReq.FinalPrompt {
		t.Fatalf("expected prompt unchanged on first turn")
	}
}

func TestApplyThinkingInjectionAppendsLatestUserPrompt(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:         true,
			thinkingInjection: boolPtr(true),
		},
		DS: ds,
	}
	req := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	stdReq, err := promptcompat.NormalizeOpenAIChatRequest(h.Store, req, "")
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	out, err := h.applyCurrentInputFile(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, stdReq)
	if err != nil {
		t.Fatalf("apply thinking injection failed: %v", err)
	}
	if len(ds.uploadCalls) != 0 {
		t.Fatalf("expected no upload for first short turn, got %d", len(ds.uploadCalls))
	}
	if !strings.Contains(out.FinalPrompt, "hello\n\n"+promptcompat.ThinkingInjectionMarker) {
		t.Fatalf("expected thinking injection after latest user message, got %s", out.FinalPrompt)
	}
}

// When tools are present and thinking is enabled (the typical Claude Code
// agent flow), the long workflow playbook must move into the system block
// so the model still sees it on no-thinking fast-path turns. The short
// reasoning-effort half stays at the user-message tail. This regression
// test pins both halves to their target locations — see Issue #18 for the
// fast-path / lost tool_use failure mode that motivated the split.
func TestApplyThinkingInjectionSplitsPlaybookToSystemWhenToolsPresent(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:         true,
			thinkingInjection: boolPtr(true),
		},
		DS: ds,
	}
	req := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{"role": "user", "content": "list the files"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "Read",
					"description": "Read a file",
					"parameters": map[string]any{
						"type":       "object",
						"properties": map[string]any{"file_path": map[string]any{"type": "string"}},
					},
				},
			},
		},
	}
	stdReq, err := promptcompat.NormalizeOpenAIChatRequest(h.Store, req, "")
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	out, err := h.applyCurrentInputFile(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, stdReq)
	if err != nil {
		t.Fatalf("apply thinking injection failed: %v", err)
	}

	// The reasoning effort marker must still anchor the latest user turn.
	userMarker := "list the files\n\n" + promptcompat.ThinkingInjectionMarker
	if !strings.Contains(out.FinalPrompt, userMarker) {
		t.Fatalf("reasoning effort missing from latest user message; final prompt=%s", out.FinalPrompt)
	}
	// The playbook (Tool-Chain Discipline) must have moved into the system
	// block (and therefore appear *before* the user marker in the prompt).
	playbookProbe := "Tool-Chain Discipline"
	if !strings.Contains(out.FinalPrompt, playbookProbe) {
		t.Fatalf("playbook missing from final prompt; got %s", out.FinalPrompt)
	}
	playbookIdx := strings.Index(out.FinalPrompt, playbookProbe)
	userIdx := strings.Index(out.FinalPrompt, userMarker)
	if playbookIdx < 0 || userIdx < 0 || playbookIdx >= userIdx {
		t.Fatalf("playbook should precede the user-tail reasoning effort marker; playbook_idx=%d user_idx=%d", playbookIdx, userIdx)
	}
	// And the user tail must NOT carry the playbook duplicate (the legacy
	// behaviour was to dump everything into the user message).
	tail := out.FinalPrompt[userIdx:]
	if strings.Contains(tail, playbookProbe) {
		t.Fatalf("playbook must not be duplicated at the user tail; tail=%s", tail)
	}
}

// Without tools the playbook is dead weight — the legacy single-block
// injection at the user tail is preserved.
func TestApplyThinkingInjectionKeepsLegacyShapeWithoutTools(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:         true,
			thinkingInjection: boolPtr(true),
		},
		DS: ds,
	}
	req := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{"role": "user", "content": "tell me a story"},
		},
	}
	stdReq, err := promptcompat.NormalizeOpenAIChatRequest(h.Store, req, "")
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	out, err := h.applyCurrentInputFile(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, stdReq)
	if err != nil {
		t.Fatalf("apply thinking injection failed: %v", err)
	}
	// Legacy contract: full default prompt at the user tail.
	if !strings.Contains(out.FinalPrompt, "tell me a story\n\n"+promptcompat.ThinkingInjectionMarker) {
		t.Fatalf("expected legacy injection at user tail without tools; got %s", out.FinalPrompt)
	}
}

func TestApplyThinkingInjectionUsesCustomPrompt(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:         true,
			thinkingInjection: boolPtr(true),
			thinkingPrompt:    "custom thinking format",
		},
		DS: ds,
	}
	req := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	stdReq, err := promptcompat.NormalizeOpenAIChatRequest(h.Store, req, "")
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	out, err := h.applyCurrentInputFile(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, stdReq)
	if err != nil {
		t.Fatalf("apply thinking injection failed: %v", err)
	}
	if !strings.Contains(out.FinalPrompt, "hello\n\ncustom thinking format") {
		t.Fatalf("expected custom thinking injection after latest user message, got %s", out.FinalPrompt)
	}
}

func TestApplyCurrentInputFileDisabledPassThrough(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			currentInputEnabled: false,
		},
		DS: ds,
	}
	req := map[string]any{
		"model":    "deepseek-v4-pro",
		"messages": historySplitTestMessages(),
	}
	stdReq, err := promptcompat.NormalizeOpenAIChatRequest(h.Store, req, "")
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	out, err := h.applyCurrentInputFile(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, stdReq)
	if err != nil {
		t.Fatalf("apply current input file failed: %v", err)
	}
	if len(ds.uploadCalls) != 0 {
		t.Fatalf("expected no uploads when both split modes are disabled, got %d", len(ds.uploadCalls))
	}
	if out.CurrentInputFileApplied || out.HistoryText != "" {
		t.Fatalf("expected direct pass-through, got current_input=%v history=%q", out.CurrentInputFileApplied, out.HistoryText)
	}
	if !strings.Contains(out.FinalPrompt, "first user turn") || !strings.Contains(out.FinalPrompt, "latest user turn") {
		t.Fatalf("expected original prompt context to stay inline, got %s", out.FinalPrompt)
	}
}

func TestApplyCurrentInputFileUploadsFirstTurnAsPlainTranscript(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			currentInputEnabled: true,
			currentInputMin:     10,
			thinkingInjection:   boolPtr(true),
		},
		DS: ds,
	}
	req := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "first turn content that is long enough"},
		},
	}
	stdReq, err := promptcompat.NormalizeOpenAIChatRequest(h.Store, req, "")
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	out, err := h.applyCurrentInputFile(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, stdReq)
	if err != nil {
		t.Fatalf("apply current input file failed: %v", err)
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected 1 current input upload, got %d", len(ds.uploadCalls))
	}
	upload := ds.uploadCalls[0]
	if upload.Filename != "DEEPSEEK_WEB_TO_API_HISTORY.txt" {
		t.Fatalf("unexpected upload filename: %q", upload.Filename)
	}
	uploadedText := string(upload.Data)
	if strings.Contains(uploadedText, "[file content end]") || strings.Contains(uploadedText, "[file name]:") || strings.Contains(uploadedText, "[file content begin]") {
		t.Fatalf("expected plain transcript without injected file wrapper, got %q", uploadedText)
	}
	for _, want := range []string{
		"# DEEPSEEK_WEB_TO_API_HISTORY.txt",
		"=== 1. USER ===",
		"first turn content that is long enough",
	} {
		if !strings.Contains(uploadedText, want) {
			t.Fatalf("expected uploaded transcript to contain %q, got %q", want, uploadedText)
		}
	}
	if !strings.Contains(uploadedText, promptcompat.ThinkingInjectionMarker) {
		t.Fatalf("expected thinking injection in current input file, got %q", uploadedText)
	}
	if strings.Contains(out.FinalPrompt, "first turn content that is long enough") {
		t.Fatalf("expected current input text to be replaced in live prompt, got %s", out.FinalPrompt)
	}
	if strings.Contains(out.FinalPrompt, "CURRENT_USER_INPUT.txt") || strings.Contains(out.FinalPrompt, "Read that file") {
		t.Fatalf("expected live prompt not to instruct file reads, got %s", out.FinalPrompt)
	}
	if !strings.Contains(out.FinalPrompt, "Continue from the latest state in the attached DEEPSEEK_WEB_TO_API_HISTORY.txt context.") {
		t.Fatalf("expected continuation-oriented prompt in live prompt, got %s", out.FinalPrompt)
	}
	if len(out.RefFileIDs) != 1 || out.RefFileIDs[0] != "file-inline-1" {
		t.Fatalf("expected current input file id in ref_file_ids, got %#v", out.RefFileIDs)
	}
	if !strings.Contains(out.PromptTokenText, "first turn content that is long enough") {
		t.Fatalf("expected prompt token text to preserve original full context, got %q", out.PromptTokenText)
	}
	if !strings.Contains(out.PromptTokenText, "# DEEPSEEK_WEB_TO_API_HISTORY.txt") || !strings.Contains(out.PromptTokenText, "=== 1. USER ===") {
		t.Fatalf("expected prompt token text to include numbered history transcript, got %q", out.PromptTokenText)
	}
}

func TestApplyCurrentInputFilePreservesFullContextPromptForTokenCounting(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			currentInputEnabled: true,
			currentInputMin:     0,
			thinkingInjection:   boolPtr(true),
		},
		DS: ds,
	}
	req := map[string]any{
		"model":    "deepseek-v4-pro",
		"messages": historySplitTestMessages(),
	}
	stdReq, err := promptcompat.NormalizeOpenAIChatRequest(h.Store, req, "")
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	out, err := h.applyCurrentInputFile(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, stdReq)
	if err != nil {
		t.Fatalf("apply current input file failed: %v", err)
	}
	if out.FinalPrompt == stdReq.FinalPrompt {
		t.Fatalf("expected live prompt to be rewritten after current input file")
	}
	// PromptTokenText must include the uploaded file content (which contains the full context)
	// plus the neutral live prompt — reflecting the actual downstream token cost.
	if !strings.Contains(out.PromptTokenText, "first user turn") || !strings.Contains(out.PromptTokenText, "latest user turn") {
		t.Fatalf("expected prompt token text to contain file context with full conversation, got %q", out.PromptTokenText)
	}
	if strings.Contains(out.PromptTokenText, "[file content end]") || strings.Contains(out.PromptTokenText, "[file name]:") {
		t.Fatalf("expected prompt token text to omit file wrapper tags, got %q", out.PromptTokenText)
	}
	if !strings.Contains(out.PromptTokenText, "# DEEPSEEK_WEB_TO_API_HISTORY.txt") || !strings.Contains(out.PromptTokenText, "=== 1. SYSTEM ===") {
		t.Fatalf("expected prompt token text to include numbered history transcript, got %q", out.PromptTokenText)
	}
	if !strings.Contains(out.PromptTokenText, "Continue from the latest state in the attached DEEPSEEK_WEB_TO_API_HISTORY.txt context.") {
		t.Fatalf("expected prompt token text to also include continuation prompt, got %q", out.PromptTokenText)
	}
	if strings.Contains(out.FinalPrompt, "first user turn") || strings.Contains(out.FinalPrompt, "latest user turn") {
		t.Fatalf("expected live prompt to hide original turns, got %q", out.FinalPrompt)
	}
}

func TestApplyCurrentInputFileUploadsFullContextFile(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			currentInputEnabled: true,
			currentInputMin:     0,
			thinkingInjection:   boolPtr(true),
		},
		DS: ds,
	}
	req := map[string]any{
		"model":    "deepseek-v4-pro",
		"messages": historySplitTestMessages(),
	}
	stdReq, err := promptcompat.NormalizeOpenAIChatRequest(h.Store, req, "")
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	out, err := h.applyCurrentInputFile(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, stdReq)
	if err != nil {
		t.Fatalf("apply current input file failed: %v", err)
	}
	if !out.CurrentInputFileApplied {
		t.Fatalf("expected current input file to apply")
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected one current input upload, got %d", len(ds.uploadCalls))
	}
	upload := ds.uploadCalls[0]
	if upload.Filename != "DEEPSEEK_WEB_TO_API_HISTORY.txt" {
		t.Fatalf("expected DEEPSEEK_WEB_TO_API_HISTORY.txt upload, got %q", upload.Filename)
	}
	// v1.0.10: vision is disabled — request now resolves to deepseek-v4-pro
	// which stamps "expert" model_type onto the upload metadata.
	if upload.ModelType != "expert" {
		t.Fatalf("expected expert model type, got %q", upload.ModelType)
	}
	uploadedText := string(upload.Data)
	for _, want := range []string{"# DEEPSEEK_WEB_TO_API_HISTORY.txt", "=== 1. SYSTEM ===", "=== 2. USER ===", "=== 3. ASSISTANT ===", "=== 4. TOOL ===", "=== 5. USER ===", "system instructions", "first user turn", "hidden reasoning", "tool result", "latest user turn", promptcompat.ThinkingInjectionMarker} {
		if !strings.Contains(uploadedText, want) {
			t.Fatalf("expected full context file to contain %q, got %q", want, uploadedText)
		}
	}
	if strings.Contains(out.FinalPrompt, "first user turn") || strings.Contains(out.FinalPrompt, "latest user turn") || strings.Contains(out.FinalPrompt, "CURRENT_USER_INPUT.txt") || strings.Contains(out.FinalPrompt, "Read that file") {
		t.Fatalf("expected live prompt to use only a continuation instruction, got %s", out.FinalPrompt)
	}
	if !strings.Contains(out.FinalPrompt, "Continue from the latest state in the attached DEEPSEEK_WEB_TO_API_HISTORY.txt context.") {
		t.Fatalf("expected continuation-oriented prompt in live prompt, got %s", out.FinalPrompt)
	}
}

func TestApplyCurrentInputFileCarriesHistoryText(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			currentInputEnabled: true,
		},
		DS: ds,
	}
	req := map[string]any{
		"model":    "deepseek-v4-flash",
		"messages": historySplitTestMessages(),
	}
	stdReq, err := promptcompat.NormalizeOpenAIChatRequest(h.Store, req, "")
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	out, err := h.applyCurrentInputFile(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, stdReq)
	if err != nil {
		t.Fatalf("apply current input file failed: %v", err)
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected 1 upload call, got %d", len(ds.uploadCalls))
	}
	if out.HistoryText != string(ds.uploadCalls[0].Data) {
		t.Fatalf("expected current input file flow to preserve uploaded text in history, got %q", out.HistoryText)
	}
	if !strings.Contains(out.HistoryText, "# DEEPSEEK_WEB_TO_API_HISTORY.txt") || !strings.Contains(out.HistoryText, "=== 1. SYSTEM ===") {
		t.Fatalf("expected history text to use numbered transcript format, got %q", out.HistoryText)
	}
}

func TestChatCompletionsCurrentInputFileUploadsContextAndKeepsNeutralPrompt(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			currentInputEnabled: true,
		},
		Auth: streamStatusAuthStub{},
		DS:   ds,
	}
	reqBody, _ := json.Marshal(map[string]any{
		"model":    "deepseek-v4-flash",
		"messages": historySplitTestMessages(),
		"stream":   false,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(reqBody)))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected 1 upload call, got %d", len(ds.uploadCalls))
	}
	upload := ds.uploadCalls[0]
	if upload.Filename != "DEEPSEEK_WEB_TO_API_HISTORY.txt" {
		t.Fatalf("unexpected upload filename: %q", upload.Filename)
	}
	if upload.Purpose != "assistants" {
		t.Fatalf("unexpected purpose: %q", upload.Purpose)
	}
	historyText := string(upload.Data)
	if strings.Contains(historyText, "[file content end]") || strings.Contains(historyText, "[file name]:") || strings.Contains(historyText, "[file content begin]") {
		t.Fatalf("expected plain transcript without injected IGNORE wrapper, got %s", historyText)
	}
	if !strings.Contains(historyText, "latest user turn") {
		t.Fatalf("expected full context to include latest turn, got %s", historyText)
	}
	if ds.completionReq == nil {
		t.Fatal("expected completion payload to be captured")
	}
	promptText, _ := ds.completionReq["prompt"].(string)
	if !strings.Contains(promptText, "Continue from the latest state in the attached DEEPSEEK_WEB_TO_API_HISTORY.txt context.") {
		t.Fatalf("expected continuation-oriented prompt, got %s", promptText)
	}
	if strings.Contains(promptText, "first user turn") || strings.Contains(promptText, "latest user turn") {
		t.Fatalf("expected prompt to hide original turns, got %s", promptText)
	}
	refIDs, _ := ds.completionReq["ref_file_ids"].([]any)
	if len(refIDs) == 0 || refIDs[0] != "file-inline-1" {
		t.Fatalf("expected uploaded current input file to be first ref_file_id, got %#v", ds.completionReq["ref_file_ids"])
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	usage, _ := body["usage"].(map[string]any)
	promptTokens := int(usage["prompt_tokens"].(float64))
	neutralCount := util.CountPromptTokens(promptText, "deepseek-v4-flash")
	if promptTokens <= neutralCount {
		t.Fatalf("expected prompt_tokens to exceed neutral live prompt count (includes file context), got=%d neutral=%d", promptTokens, neutralCount)
	}
}

func TestChatCompletionsSkippedCurrentInputFileKeepsLivePrompt(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			currentInputEnabled: true,
		},
		Auth: streamStatusAuthStub{},
		DS:   ds,
	}
	reqBody, _ := json.Marshal(map[string]any{
		"model":    "deepseek-v4-flash",
		"messages": historySplitTestMessages(),
		"stream":   false,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(reqBody)))
	req = req.WithContext(openaishared.WithCurrentInputFileSkipped(req.Context()))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(ds.uploadCalls) != 0 {
		t.Fatalf("expected no current input upload, got %d", len(ds.uploadCalls))
	}
	if ds.completionReq == nil {
		t.Fatal("expected completion payload to be captured")
	}
	promptText, _ := ds.completionReq["prompt"].(string)
	if !strings.Contains(promptText, "latest user turn") || !strings.Contains(promptText, "tool result") {
		t.Fatalf("expected live prompt to keep original conversation context, got %s", promptText)
	}
	if strings.Contains(promptText, "Answer the latest user request directly.") {
		t.Fatalf("expected live prompt to avoid neutral current input prompt, got %s", promptText)
	}
	if refIDs, ok := ds.completionReq["ref_file_ids"].([]any); ok && len(refIDs) > 0 {
		t.Fatalf("expected no current input file ref, got %#v", ds.completionReq["ref_file_ids"])
	}
}

func TestResponsesCurrentInputFileUploadsContextAndKeepsNeutralPrompt(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			currentInputEnabled: true,
		},
		Auth: streamStatusAuthStub{},
		DS:   ds,
	}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)
	reqBody, _ := json.Marshal(map[string]any{
		"model":    "deepseek-v4-flash",
		"messages": historySplitTestMessages(),
		"stream":   false,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(reqBody)))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected 1 upload call, got %d", len(ds.uploadCalls))
	}
	historyText := string(ds.uploadCalls[0].Data)
	if !strings.Contains(historyText, "# DEEPSEEK_WEB_TO_API_HISTORY.txt") || !strings.Contains(historyText, "=== 1. SYSTEM ===") {
		t.Fatalf("expected uploaded history text to use numbered transcript format, got %s", historyText)
	}
	if ds.completionReq == nil {
		t.Fatal("expected completion payload to be captured")
	}
	promptText, _ := ds.completionReq["prompt"].(string)
	if !strings.Contains(promptText, "Continue from the latest state in the attached DEEPSEEK_WEB_TO_API_HISTORY.txt context.") {
		t.Fatalf("expected continuation-oriented prompt, got %s", promptText)
	}
	if strings.Contains(promptText, "first user turn") || strings.Contains(promptText, "latest user turn") {
		t.Fatalf("expected prompt to hide original turns, got %s", promptText)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	usage, _ := body["usage"].(map[string]any)
	inputTokens := int(usage["input_tokens"].(float64))
	neutralCount := util.CountPromptTokens(promptText, "deepseek-v4-flash")
	if inputTokens <= neutralCount {
		t.Fatalf("expected input_tokens to exceed neutral live prompt count (includes file context), got=%d neutral=%d", inputTokens, neutralCount)
	}
}

func TestChatCompletionsCurrentInputFileMapsManagedAuthFailureTo401(t *testing.T) {
	ds := &inlineUploadDSStub{
		uploadErr: &dsclient.RequestFailure{Op: "upload file", Kind: dsclient.FailureManagedUnauthorized, Message: "expired token"},
	}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			currentInputEnabled: true,
		},
		Auth: streamStatusManagedAuthStub{},
		DS:   ds,
	}
	reqBody, _ := json.Marshal(map[string]any{
		"model":    "deepseek-v4-flash",
		"messages": historySplitTestMessages(),
		"stream":   false,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(reqBody)))
	req.Header.Set("Authorization", "Bearer managed-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Please re-login the account in admin") {
		t.Fatalf("expected managed auth error message, got %s", rec.Body.String())
	}
}

func TestResponsesCurrentInputFileMapsDirectAuthFailureTo401(t *testing.T) {
	ds := &inlineUploadDSStub{
		uploadErr: &dsclient.RequestFailure{Op: "upload file", Kind: dsclient.FailureDirectUnauthorized, Message: "invalid token"},
	}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			currentInputEnabled: true,
		},
		Auth: streamStatusAuthStub{},
		DS:   ds,
	}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)
	reqBody, _ := json.Marshal(map[string]any{
		"model":    "deepseek-v4-flash",
		"messages": historySplitTestMessages(),
		"stream":   false,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(reqBody)))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Invalid token") {
		t.Fatalf("expected direct auth error message, got %s", rec.Body.String())
	}
}

func TestChatCompletionsCurrentInputFileUploadFailureReturnsInternalServerError(t *testing.T) {
	ds := &inlineUploadDSStub{uploadErr: errors.New("boom")}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			currentInputEnabled: true,
		},
		Auth: streamStatusAuthStub{},
		DS:   ds,
	}
	reqBody, _ := json.Marshal(map[string]any{
		"model":    "deepseek-v4-flash",
		"messages": historySplitTestMessages(),
		"stream":   false,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(reqBody)))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCurrentInputFileWorksAcrossAutoDeleteModes(t *testing.T) {
	for _, mode := range []string{"none", "single", "all"} {
		t.Run(mode, func(t *testing.T) {
			ds := &inlineUploadDSStub{}
			h := &openAITestSurface{
				Store: mockOpenAIConfig{
					wideInput:           true,
					autoDeleteMode:      mode,
					currentInputEnabled: true,
				},
				Auth: streamStatusAuthStub{},
				DS:   ds,
			}
			reqBody, _ := json.Marshal(map[string]any{
				"model":    "deepseek-v4-flash",
				"messages": historySplitTestMessages(),
				"stream":   false,
			})
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(reqBody)))
			req.Header.Set("Authorization", "Bearer direct-token")
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			h.ChatCompletions(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
			}
			if len(ds.uploadCalls) != 1 {
				t.Fatalf("expected current input upload for mode=%s, got %d", mode, len(ds.uploadCalls))
			}
			historyText := string(ds.uploadCalls[0].Data)
			if !strings.Contains(historyText, "# DEEPSEEK_WEB_TO_API_HISTORY.txt") || !strings.Contains(historyText, "=== 1. SYSTEM ===") {
				t.Fatalf("expected uploaded history text to use numbered transcript format, got %s", historyText)
			}
			if ds.completionReq == nil {
				t.Fatalf("expected completion payload for mode=%s", mode)
			}
			promptText, _ := ds.completionReq["prompt"].(string)
			if !strings.Contains(promptText, "Continue from the latest state in the attached DEEPSEEK_WEB_TO_API_HISTORY.txt context.") || strings.Contains(promptText, "first user turn") || strings.Contains(promptText, "latest user turn") {
				t.Fatalf("unexpected prompt for mode=%s: %s", mode, promptText)
			}
		})
	}
}

func defaultToolChoicePolicy() promptcompat.ToolChoicePolicy {
	return promptcompat.DefaultToolChoicePolicy()
}

func boolPtr(v bool) *bool {
	return &v
}

// makeLargeInlineMessages produces a message slice whose transcript form is
// at least `targetBytes` long — enough for splitCurrentInputPrefixTail to
// pick a clean cut point. The first user message ("seed user request")
// satisfies the latest-user threshold trigger; the synthetic assistant
// blob carries the bulk so the prefix grows past the 32 KB target.
func makeLargeInlineMessages(targetBytes int) []any {
	bigBlob := strings.Repeat("0123456789abcdef ", targetBytes/16+8)
	return []any{
		map[string]any{"role": "system", "content": "you are a tester"},
		map[string]any{"role": "user", "content": "seed user request " + strings.Repeat("x", 200)},
		map[string]any{"role": "assistant", "content": bigBlob},
		map[string]any{"role": "user", "content": "next user turn"},
	}
}

// TestApplyCurrentInputFileInlinePrefixUsesStructuredBody confirms the
// new inline-prefix mode (RemoteFileUpload disabled) stops calling the
// upload API and emits a single user message whose body is structured
// with a "RECENT CONVERSATION TURNS" boundary marker. First turn is a
// CheckpointRefresh (cache miss); a follow-up turn with the same prefix
// hits the cache (Reused=true) and skips the marker shuffle.
func TestApplyCurrentInputFileInlinePrefixUsesStructuredBody(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			currentInputEnabled: true,
			currentInputMin:     100,
			remoteFileUpload:    boolPtr(false), // inline mode (our v1.0.3+ default)
		},
		DS: ds,
	}
	req := map[string]any{
		"model":    "deepseek-v4-pro",
		"messages": makeLargeInlineMessages(40 * 1024),
	}
	stdReq, err := promptcompat.NormalizeOpenAIChatRequest(h.Store, req, "")
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	out, err := h.applyCurrentInputFile(context.Background(),
		&auth.RequestAuth{DeepSeekToken: "tk-inline-prefix", SessionKey: "sess-A", CallerID: "caller-A", AccountID: "acct-A"},
		stdReq)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if len(ds.uploadCalls) != 0 {
		t.Fatalf("inline mode must NOT call upload_file; got %d uploads", len(ds.uploadCalls))
	}
	if !out.CurrentInputFileApplied {
		t.Fatalf("CurrentInputFileApplied should be true after inline-prefix path")
	}
	if !out.CurrentInputCheckpointRefresh {
		t.Fatalf("first turn must mark CheckpointRefresh=true; got false")
	}
	if out.CurrentInputPrefixReused {
		t.Fatalf("first turn must NOT reuse a prefix; got Reused=true")
	}
	if out.CurrentInputPrefixChars <= 0 {
		t.Fatalf("first turn must record PrefixChars>0; got %d", out.CurrentInputPrefixChars)
	}
	if out.CurrentInputTailChars <= 0 {
		t.Fatalf("first turn must record TailChars>0; got %d", out.CurrentInputTailChars)
	}
	if !strings.Contains(out.FinalPrompt, "--- RECENT CONVERSATION TURNS ---") {
		t.Fatalf("inline-prefix body must contain the recent-turns marker; got %q", out.FinalPrompt)
	}
	if !strings.Contains(out.FinalPrompt, "--- INSTRUCTION ---") {
		t.Fatalf("inline-prefix body must contain the instruction marker; got %q", out.FinalPrompt)
	}

	// Second turn: same session + extended transcript starting with the
	// same stable prefix bytes → expect Reused=true, no upload, no
	// checkpoint refresh.
	follow := append([]any(nil), req["messages"].([]any)...)
	follow = append(follow, map[string]any{"role": "assistant", "content": "ack"}, map[string]any{"role": "user", "content": "second user turn after the prefix anchor"})
	req2 := map[string]any{"model": "deepseek-v4-pro", "messages": follow}
	stdReq2, err := promptcompat.NormalizeOpenAIChatRequest(h.Store, req2, "")
	if err != nil {
		t.Fatalf("normalize follow-up failed: %v", err)
	}
	out2, err := h.applyCurrentInputFile(context.Background(),
		&auth.RequestAuth{DeepSeekToken: "tk-inline-prefix", SessionKey: "sess-A", CallerID: "caller-A", AccountID: "acct-A"},
		stdReq2)
	if err != nil {
		t.Fatalf("apply follow-up failed: %v", err)
	}
	if len(ds.uploadCalls) != 0 {
		t.Fatalf("follow-up turn still must not upload; got %d uploads", len(ds.uploadCalls))
	}
	if !out2.CurrentInputPrefixReused {
		t.Fatalf("follow-up turn with matching prefix must mark Reused=true; got false")
	}
	if out2.CurrentInputCheckpointRefresh {
		t.Fatalf("follow-up turn must not be a CheckpointRefresh; got true")
	}
	if out2.CurrentInputPrefixHash != out.CurrentInputPrefixHash {
		t.Fatalf("prefix hash should be stable across turns; first=%q follow=%q", out.CurrentInputPrefixHash, out2.CurrentInputPrefixHash)
	}
}

// TestApplyCurrentInputFileInlinePrefixKeepsOlderVariantOnRefresh confirms
// the multi-variant chain: when a session's transcript outgrows
// currentInputMaxTailChars and forces a checkpoint refresh, the OLDER
// prefix is NOT discarded — it stays in the chain so a follow-up turn
// whose tail still fits the old prefix can still reuse it.
func TestApplyCurrentInputFileInlinePrefixKeepsOlderVariantOnRefresh(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			currentInputEnabled: true,
			currentInputMin:     100,
			remoteFileUpload:    boolPtr(false),
		},
		DS: ds,
	}
	authT := &auth.RequestAuth{DeepSeekToken: "tk-multi", SessionKey: "sess-multi", CallerID: "caller-multi", AccountID: "acct-multi"}

	// Turn 1: large transcript → produce prefix P1.
	turn1Msgs := makeLargeInlineMessages(40 * 1024)
	stdReq, _ := promptcompat.NormalizeOpenAIChatRequest(h.Store, map[string]any{"model": "deepseek-v4-pro", "messages": turn1Msgs}, "")
	out1, err := h.applyCurrentInputFile(context.Background(), authT, stdReq)
	if err != nil {
		t.Fatalf("turn1 failed: %v", err)
	}
	if !out1.CurrentInputCheckpointRefresh {
		t.Fatalf("turn1 expected to be checkpoint refresh; got reused=%v", out1.CurrentInputPrefixReused)
	}
	hashP1 := out1.CurrentInputPrefixHash

	// Turn 2: same prefix bytes + small new tail → reuses P1.
	turn2Msgs := append([]any(nil), turn1Msgs...)
	turn2Msgs = append(turn2Msgs, map[string]any{"role": "assistant", "content": "ack"}, map[string]any{"role": "user", "content": "tiny new turn"})
	stdReq, _ = promptcompat.NormalizeOpenAIChatRequest(h.Store, map[string]any{"model": "deepseek-v4-pro", "messages": turn2Msgs}, "")
	out2, err := h.applyCurrentInputFile(context.Background(), authT, stdReq)
	if err != nil {
		t.Fatalf("turn2 failed: %v", err)
	}
	if !out2.CurrentInputPrefixReused {
		t.Fatalf("turn2 expected to reuse P1; got CheckpointRefresh=%v hash=%q", out2.CurrentInputCheckpointRefresh, out2.CurrentInputPrefixHash)
	}
	if out2.CurrentInputPrefixHash != hashP1 {
		t.Fatalf("turn2 hash mismatch: want=%q got=%q", hashP1, out2.CurrentInputPrefixHash)
	}

	// Turn 3: bloat the transcript with ~150 KB extra so tail-after-P1
	// exceeds currentInputMaxTailChars (128 KB) → forces a refresh
	// producing a new prefix P2. The chain should NOW hold both P1 and P2.
	turn3Msgs := append([]any(nil), turn2Msgs...)
	turn3Msgs = append(turn3Msgs,
		map[string]any{"role": "assistant", "content": strings.Repeat("FILLER ", 22000)},
		map[string]any{"role": "user", "content": "user turn after bloat"})
	stdReq, _ = promptcompat.NormalizeOpenAIChatRequest(h.Store, map[string]any{"model": "deepseek-v4-pro", "messages": turn3Msgs}, "")
	out3, err := h.applyCurrentInputFile(context.Background(), authT, stdReq)
	if err != nil {
		t.Fatalf("turn3 failed: %v", err)
	}
	if !out3.CurrentInputCheckpointRefresh {
		t.Fatalf("turn3 expected checkpoint refresh after bloat; got reused=%v", out3.CurrentInputPrefixReused)
	}
	hashP2 := out3.CurrentInputPrefixHash
	if hashP2 == hashP1 {
		t.Fatalf("turn3 expected NEW prefix P2 distinct from P1; both = %q", hashP1)
	}

	// Turn 4: replay turn-2-shape transcript (small tail above P1). The
	// chain still has P1 → expect reuse, NOT another fresh refresh.
	stdReq, _ = promptcompat.NormalizeOpenAIChatRequest(h.Store, map[string]any{"model": "deepseek-v4-pro", "messages": turn2Msgs}, "")
	out4, err := h.applyCurrentInputFile(context.Background(), authT, stdReq)
	if err != nil {
		t.Fatalf("turn4 failed: %v", err)
	}
	if !out4.CurrentInputPrefixReused {
		t.Fatalf("turn4 expected to reuse P1 from older chain slot; got refresh hash=%q", out4.CurrentInputPrefixHash)
	}
	if out4.CurrentInputPrefixHash != hashP1 {
		t.Fatalf("turn4 should reuse P1 specifically; want %q got %q", hashP1, out4.CurrentInputPrefixHash)
	}
}

// TestApplyCurrentInputFileInlinePrefixIsolatesFromFileMode confirms the
// store's mode tag prevents an inline-mode cached entry from being
// reused as if it had a file_id when the same session later flips to
// file mode.
func TestApplyCurrentInputFileInlinePrefixIsolatesFromFileMode(t *testing.T) {
	dsInline := &inlineUploadDSStub{}
	hInline := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			currentInputEnabled: true,
			currentInputMin:     100,
			remoteFileUpload:    boolPtr(false),
		},
		DS: dsInline,
	}
	req := map[string]any{
		"model":    "deepseek-v4-pro",
		"messages": makeLargeInlineMessages(40 * 1024),
	}
	stdReq, _ := promptcompat.NormalizeOpenAIChatRequest(hInline.Store, req, "")
	auth1 := &auth.RequestAuth{DeepSeekToken: "tk-iso", SessionKey: "sess-iso", CallerID: "caller-iso", AccountID: "acct-iso"}
	if _, err := hInline.applyCurrentInputFile(context.Background(), auth1, stdReq); err != nil {
		t.Fatalf("inline first turn failed: %v", err)
	}

	// Now the same session re-enters via a file-mode handler. The cached
	// inline-mode entry must NOT satisfy a file-mode cache hit (no
	// FileID), so we expect a fresh upload to happen.
	dsFile := &inlineUploadDSStub{}
	hFile := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			currentInputEnabled: true,
			currentInputMin:     100,
			remoteFileUpload:    boolPtr(true),
		},
		DS: dsFile,
	}
	stdReq2, _ := promptcompat.NormalizeOpenAIChatRequest(hFile.Store, req, "")
	if _, err := hFile.applyCurrentInputFile(context.Background(), auth1, stdReq2); err != nil {
		t.Fatalf("file-mode turn failed: %v", err)
	}
	if len(dsFile.uploadCalls) == 0 {
		t.Fatalf("switching to file mode must trigger a fresh upload; got 0 uploads (inline cache leaked across modes)")
	}
}
