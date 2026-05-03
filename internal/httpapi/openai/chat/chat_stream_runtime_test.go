package chat

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"DeepSeek_Web_To_API/internal/sse"
)

func TestChatStreamRuntimeCoalescesParsedTextParts(t *testing.T) {
	rec := httptest.NewRecorder()
	rt := newChatStreamRuntime(
		rec,
		http.NewResponseController(rec),
		false,
		"chatcmpl-test",
		123,
		"deepseek-v4-flash",
		"prompt",
		false,
		false,
		false,
		false,
		nil,
		nil,
		false,
		false,
		false,
	)

	rt.onParsed(sse.LineResult{
		Parsed: true,
		Parts: []sse.ContentPart{
			{Type: "text", Text: "我是DeepSeek"},
			{Type: "text", Text: "公司开发的"},
			{Type: "text", Text: "人工智能助手。"},
		},
	})

	frames := openAIStreamDataFrames(t, rec.Body.String())
	if len(frames) != 1 {
		t.Fatalf("expected 1 coalesced stream data frame, got %d body=%s", len(frames), rec.Body.String())
	}
	var reconstructed strings.Builder
	for _, frame := range frames {
		var obj map[string]any
		if err := json.Unmarshal([]byte(frame), &obj); err != nil {
			t.Fatalf("decode frame failed: %v frame=%s", err, frame)
		}
		choices, ok := obj["choices"].([]any)
		if !ok || len(choices) != 1 {
			t.Fatalf("expected exactly one choice per chunk, got %#v", obj["choices"])
		}
		choice, _ := choices[0].(map[string]any)
		delta, _ := choice["delta"].(map[string]any)
		if text, _ := delta["content"].(string); text != "" {
			reconstructed.WriteString(text)
		}
	}
	if got := reconstructed.String(); got != "我是DeepSeek公司开发的人工智能助手。" {
		t.Fatalf("unexpected reconstructed stream content: %q", got)
	}
}

func openAIStreamDataFrames(t *testing.T, body string) []string {
	t.Helper()
	var frames []string
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		frames = append(frames, payload)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan stream frames failed: %v", err)
	}
	return frames
}
