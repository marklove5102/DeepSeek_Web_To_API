package promptcompat

import "testing"

// v1.0.19: ExtractLatestUserText is the input source for LLM safety
// review. The audit must see only the human's last turn — passing the
// FinalPrompt (system prompts + history + gateway injections) was
// causing the audit LLM to flag legitimate short prompts because it
// saw the gateway's own instructions and treated them as user content.
func TestExtractLatestUserTextString(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "system", "content": "you are a helpful assistant"},
		map[string]any{"role": "user", "content": "first question"},
		map[string]any{"role": "assistant", "content": "first answer"},
		map[string]any{"role": "user", "content": "second question"},
	}
	if got := ExtractLatestUserText(msgs); got != "second question" {
		t.Errorf("ExtractLatestUserText = %q, want %q", got, "second question")
	}
}

func TestExtractLatestUserTextOpenAIBlockArray(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "hello"},
			map[string]any{"type": "text", "text": "world"},
		}},
	}
	if got := ExtractLatestUserText(msgs); got != "hello\nworld" {
		t.Errorf("ExtractLatestUserText = %q, want %q", got, "hello\nworld")
	}
}

func TestExtractLatestUserTextResponsesInputText(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "input_text", "text": "responses-style block"},
		}},
	}
	if got := ExtractLatestUserText(msgs); got != "responses-style block" {
		t.Errorf("ExtractLatestUserText = %q, want %q", got, "responses-style block")
	}
}

func TestExtractLatestUserTextSkipsNonTextBlocks(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "describe this image"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/..."}},
		}},
	}
	if got := ExtractLatestUserText(msgs); got != "describe this image" {
		t.Errorf("ExtractLatestUserText = %q, want %q", got, "describe this image")
	}
}

func TestExtractLatestUserTextNoUserMessage(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "system", "content": "policy"},
		map[string]any{"role": "assistant", "content": "auto-greet"},
	}
	if got := ExtractLatestUserText(msgs); got != "" {
		t.Errorf("ExtractLatestUserText = %q, want empty (fallback to FinalPrompt)", got)
	}
}

func TestExtractLatestUserTextEmptySlice(t *testing.T) {
	if got := ExtractLatestUserText(nil); got != "" {
		t.Errorf("ExtractLatestUserText(nil) = %q, want empty", got)
	}
	if got := ExtractLatestUserText([]any{}); got != "" {
		t.Errorf("ExtractLatestUserText([]) = %q, want empty", got)
	}
}
