package claude

import (
	"encoding/json"
	"strings"

	"DeepSeek_Web_To_API/internal/util"
)

func applyClaudeThinkingPolicyToOpenAIRequest(translated []byte, original map[string]any, stream bool) ([]byte, bool) {
	req := map[string]any{}
	if err := json.Unmarshal(translated, &req); err != nil {
		return translated, false
	}
	enabled, ok := util.ResolveThinkingOverride(original)
	if !ok {
		if _, translatedHasOverride := util.ResolveThinkingOverride(req); translatedHasOverride {
			return translated, false
		}
		enabled = !stream
	}
	typ := "disabled"
	if enabled {
		typ = "enabled"
	}
	req["thinking"] = map[string]any{"type": typ}
	out, err := json.Marshal(req)
	if err != nil {
		return translated, ok && enabled
	}
	return out, ok && enabled
}

func stripClaudeThinkingBlocks(raw []byte) []byte {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return raw
	}
	content, _ := payload["content"].([]any)
	if len(content) == 0 {
		return raw
	}
	filtered := make([]any, 0, len(content))
	for _, item := range content {
		block, _ := item.(map[string]any)
		blockType, _ := block["type"].(string)
		if strings.TrimSpace(blockType) == "thinking" {
			continue
		}
		filtered = append(filtered, item)
	}
	payload["content"] = filtered
	out, err := json.Marshal(payload)
	if err != nil {
		return raw
	}
	return out
}
