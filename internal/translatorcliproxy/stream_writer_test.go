package translatorcliproxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
)

func TestOpenAIStreamTranslatorWriterClaude(t *testing.T) {
	original := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	translated := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"stream":true}`)

	rec := httptest.NewRecorder()
	w := NewOpenAIStreamTranslatorWriter(rec, sdktranslator.FormatClaude, "claude-sonnet-4-5", original, translated)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"claude-sonnet-4-5\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n"))
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"claude-sonnet-4-5\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"))
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"claude-sonnet-4-5\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":29,\"total_tokens\":40}}\n\n"))
	_, _ = w.Write([]byte("data: [DONE]\n\n"))

	body := rec.Body.String()
	if !strings.Contains(body, "event: message_start") {
		t.Fatalf("expected claude message_start event, got: %s", body)
	}
	if !strings.Contains(body, `"output_tokens":29`) {
		t.Fatalf("expected claude stream usage to preserve output tokens, got: %s", body)
	}
}

func TestOpenAIStreamTranslatorWriterGemini(t *testing.T) {
	original := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	translated := []byte(`{"model":"gemini-2.5-pro","messages":[{"role":"user","content":"hi"}],"stream":true}`)

	rec := httptest.NewRecorder()
	w := NewOpenAIStreamTranslatorWriter(rec, sdktranslator.FormatGemini, "gemini-2.5-pro", original, translated)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gemini-2.5-pro\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"))
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gemini-2.5-pro\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":29,\"total_tokens\":40}}\n\n"))
	_, _ = w.Write([]byte("data: [DONE]\n\n"))

	body := rec.Body.String()
	if !strings.Contains(body, "candidates") {
		t.Fatalf("expected gemini stream payload, got: %s", body)
	}
	if !strings.Contains(body, `"promptTokenCount":11`) || !strings.Contains(body, `"candidatesTokenCount":29`) {
		t.Fatalf("expected gemini stream usageMetadata to preserve usage, got: %s", body)
	}
}

func TestOpenAIStreamTranslatorWriterPreservesKeepAliveComment(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewOpenAIStreamTranslatorWriter(rec, sdktranslator.FormatGemini, "gemini-2.5-pro", []byte(`{}`), []byte(`{}`))
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	_, _ = w.Write([]byte(": keep-alive\n\n"))

	body := rec.Body.String()
	if !strings.Contains(body, ": keep-alive\n\n") {
		t.Fatalf("expected keep-alive comment passthrough, got %q", body)
	}
}

func TestOpenAIStreamTranslatorWriterClaudeConvertsKeepAliveToPing(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewOpenAIStreamTranslatorWriter(rec, sdktranslator.FormatClaude, "claude-sonnet-4-5", []byte(`{}`), []byte(`{}`))
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	_, _ = w.Write([]byte(": keep-alive\n\n"))

	body := rec.Body.String()
	if !strings.Contains(body, "event: ping") || !strings.Contains(body, `"type":"ping"`) {
		t.Fatalf("expected claude ping event for keep-alive, got %q", body)
	}
	if strings.Contains(body, ": keep-alive") {
		t.Fatalf("did not expect raw keep-alive comment in claude stream, got %q", body)
	}
}

func TestOpenAIStreamTranslatorWriterClaudeConvertsStreamErrorChunk(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewOpenAIStreamTranslatorWriter(rec, sdktranslator.FormatClaude, "claude-sonnet-4-5", []byte(`{}`), []byte(`{}`))
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)

	_, _ = w.Write([]byte(`data: {"status_code":429,"error":{"message":"Upstream account hit a rate limit and returned empty output.","type":"rate_limit_error","code":"upstream_empty_output"}}` + "\n\n"))

	body := rec.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Fatalf("expected claude error event, got %q", body)
	}
	if !strings.Contains(body, `"type":"error"`) || !strings.Contains(body, `"rate_limit_error"`) {
		t.Fatalf("expected anthropic error payload, got %q", body)
	}
	if !strings.Contains(body, "Upstream account hit a rate limit") {
		t.Fatalf("expected upstream error message preserved, got %q", body)
	}
	if strings.Contains(body, `"status_code":429`) {
		t.Fatalf("did not expect raw openai error chunk passthrough, got %q", body)
	}
}

func TestOpenAIStreamTranslatorWriterClaudeConvertsHTTPErrorBody(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewOpenAIStreamTranslatorWriter(rec, sdktranslator.FormatClaude, "claude-sonnet-4-5", []byte(`{}`), []byte(`{}`))
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusTooManyRequests)

	_, _ = w.Write([]byte(`{"error":{"message":"upstream account rate limited","type":"rate_limit_error","code":"rate_limit_exceeded"}}`))

	body := rec.Body.String()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected json content-type for claude http error, got %q", contentType)
	}
	if !strings.Contains(body, `"type":"error"`) || !strings.Contains(body, `"rate_limit_error"`) {
		t.Fatalf("expected anthropic error body, got %q", body)
	}
	if strings.Contains(body, `"code":"rate_limit_exceeded"`) {
		t.Fatalf("did not expect raw openai error body passthrough, got %q", body)
	}
}

func TestInjectStreamUsageMetadataPreservesSSEFrameTerminator(t *testing.T) {
	chunk := []byte("data: {\"candidates\":[{\"index\":0}],\"model\":\"gemini-2.5-pro\"}\n\n")
	usage := openAIUsage{PromptTokens: 11, CompletionTokens: 29, TotalTokens: 40}
	got := injectStreamUsageMetadata(chunk, sdktranslator.FormatGemini, usage)
	if !strings.HasSuffix(string(got), "\n\n") {
		t.Fatalf("expected injected chunk to preserve \\n\\n frame terminator, got %q", string(got))
	}
	if !strings.Contains(string(got), `"usageMetadata"`) {
		t.Fatalf("expected usageMetadata injected, got %q", string(got))
	}
}

func TestExtractOpenAIUsageSupportsResponsesUsageFields(t *testing.T) {
	line := []byte(`data: {"usage":{"input_tokens":"11","output_tokens":"29","total_tokens":"40"}}`)
	got, ok := extractOpenAIUsage(line)
	if !ok {
		t.Fatal("expected usage extracted from input/output usage fields")
	}
	if got.PromptTokens != 11 || got.CompletionTokens != 29 || got.TotalTokens != 40 {
		t.Fatalf("unexpected usage extracted: %#v", got)
	}
}
