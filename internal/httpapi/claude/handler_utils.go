package claude

import (
	"DeepSeek_Web_To_API/internal/toolcall"
	"encoding/json"
	"fmt"
	"strings"

	"DeepSeek_Web_To_API/internal/prompt"
)

func normalizeClaudeMessages(messages []any) []any {
	out := make([]any, 0, len(messages))
	state := &claudeToolCallState{
		nameByID:       map[string]string{},
		lastIDByName:   map[string]string{},
		callIDSequence: 0,
	}
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", msg["role"])))
		switch content := msg["content"].(type) {
		case []any:
			textParts := make([]string, 0, len(content))
			flushText := func() {
				if len(textParts) == 0 {
					return
				}
				out = append(out, map[string]any{
					"role":    role,
					"content": strings.Join(textParts, "\n"),
				})
				textParts = textParts[:0]
			}
			for _, block := range content {
				b, ok := block.(map[string]any)
				if !ok {
					continue
				}
				typeStr := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", b["type"])))
				switch typeStr {
				case "text":
					if t, ok := b["text"].(string); ok {
						textParts = append(textParts, t)
					}
				case "thinking":
					if role == "assistant" {
						if t := firstClaudeBlockText(b, "thinking", "text", "content"); t != "" {
							textParts = append(textParts, "[thinking]\n"+t+"\n[/thinking]")
						}
					}
				case "redacted_thinking", "cache_edits":
					continue
				case "tool_use":
					if role == "assistant" {
						flushText()
						if toolMsg := normalizeClaudeToolUseToAssistant(b, state); toolMsg != nil {
							out = append(out, toolMsg)
						}
						continue
					}
					if raw := strings.TrimSpace(formatClaudeUnknownBlockForPrompt(b)); raw != "" {
						textParts = append(textParts, raw)
					}
				case "tool_result":
					flushText()
					if toolMsg := normalizeClaudeToolResultToToolMessage(b, state); toolMsg != nil {
						out = append(out, toolMsg)
					}
				default:
					if raw := strings.TrimSpace(formatClaudeUnknownBlockForPrompt(b)); raw != "" {
						textParts = append(textParts, raw)
					}
				}
			}
			flushText()
		default:
			copied := cloneMap(msg)
			out = append(out, copied)
		}
	}
	return out
}

func buildClaudeToolPrompt(tools []any) string {
	toolSchemas := make([]string, 0, len(tools))
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		m, ok := t.(map[string]any)
		if !ok {
			continue
		}
		name, desc, schemaObj := extractClaudeToolMeta(m)
		if name == "" {
			continue
		}
		names = append(names, name)
		schema, _ := json.Marshal(schemaObj)
		toolSchemas = append(toolSchemas, fmt.Sprintf("Tool: %s\nDescription: %s\nParameters: %s", name, desc, schema))
	}
	if len(toolSchemas) == 0 {
		return ""
	}
	return "You have access to these tools:\n\n" +
		strings.Join(toolSchemas, "\n\n") + "\n\n" +
		toolcall.BuildToolCallInstructions(names)
}

//nolint:unused // retained for compatibility with pending Claude tool-result prompt flow.
func formatClaudeToolResultForPrompt(block map[string]any) string {
	if block == nil {
		return ""
	}
	payload := map[string]any{
		"type":    "tool_result",
		"content": block["content"],
	}
	if toolCallID := strings.TrimSpace(fmt.Sprintf("%v", block["tool_use_id"])); toolCallID != "" {
		payload["tool_call_id"] = toolCallID
	} else if toolCallID := strings.TrimSpace(fmt.Sprintf("%v", block["tool_call_id"])); toolCallID != "" {
		payload["tool_call_id"] = toolCallID
	}
	if name := strings.TrimSpace(fmt.Sprintf("%v", block["name"])); name != "" {
		payload["name"] = name
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return strings.TrimSpace(fmt.Sprintf("%v", payload))
	}
	return string(b)
}

func normalizeClaudeToolUseToAssistant(block map[string]any, state *claudeToolCallState) map[string]any {
	if block == nil {
		return nil
	}
	name := strings.TrimSpace(fmt.Sprintf("%v", block["name"]))
	if name == "" {
		return nil
	}
	callID := safeStringValue(block["id"])
	if callID == "" {
		callID = safeStringValue(block["tool_use_id"])
	}
	if callID == "" {
		callID = state.nextID()
	}
	state.nameByID[callID] = name
	state.lastIDByName[strings.ToLower(name)] = callID
	arguments := block["input"]
	if arguments == nil {
		arguments = map[string]any{}
	}
	argsJSON, err := json.Marshal(arguments)
	if err != nil || len(argsJSON) == 0 {
		argsJSON = []byte("{}")
	}
	toolCalls := []any{
		map[string]any{
			"id":   callID,
			"type": "function",
			"function": map[string]any{
				"name":      name,
				"arguments": string(argsJSON),
			},
		},
	}
	return map[string]any{
		"role":       "assistant",
		"content":    prompt.FormatToolCallsForPrompt(toolCalls),
		"tool_calls": toolCalls,
	}
}

func normalizeClaudeToolResultToToolMessage(block map[string]any, state *claudeToolCallState) map[string]any {
	if block == nil {
		return nil
	}
	name := safeStringValue(block["name"])
	toolCallID := safeStringValue(block["tool_use_id"])
	if toolCallID == "" {
		toolCallID = safeStringValue(block["tool_call_id"])
	}
	if toolCallID == "" {
		if name != "" {
			toolCallID = strings.TrimSpace(state.lastIDByName[strings.ToLower(name)])
		}
	}
	if toolCallID == "" {
		toolCallID = state.nextID()
	}
	out := map[string]any{
		"role":         "tool",
		"tool_call_id": toolCallID,
		"content":      normalizeClaudeToolResultContent(block),
	}
	if name != "" {
		out["name"] = name
		state.nameByID[toolCallID] = name
		state.lastIDByName[strings.ToLower(name)] = toolCallID
	} else if inferred := strings.TrimSpace(state.nameByID[toolCallID]); inferred != "" {
		out["name"] = inferred
	}
	return out
}

func normalizeClaudeToolResultContent(block map[string]any) string {
	content := block["content"]
	toolName := safeStringValue(block["name"])
	if text, ok := content.(string); ok {
		if strings.TrimSpace(text) == "" {
			return emptyClaudeToolResultMessage(toolName)
		}
		return text
	}
	if _, ok := content.([]any); ok {
		if parts := strings.TrimSpace(normalizeClaudeToolResultContentParts(content)); parts != "" {
			return parts
		}
		return emptyClaudeToolResultMessage(toolName)
	}
	payload := map[string]any{
		"type":    "tool_result",
		"content": content,
	}
	b, err := json.Marshal(sanitizeClaudeBlockForPrompt(payload))
	if err != nil {
		fallback := strings.TrimSpace(fmt.Sprintf("%v", content))
		if fallback == "" || fallback == "<nil>" {
			return emptyClaudeToolResultMessage(toolName)
		}
		return fallback
	}
	if strings.TrimSpace(string(b)) == "" || string(b) == `{"content":null,"type":"tool_result"}` {
		return emptyClaudeToolResultMessage(toolName)
	}
	return string(b)
}

func normalizeClaudeToolResultContentParts(content any) string {
	items, ok := content.([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		switch v := item.(type) {
		case string:
			if text := strings.TrimSpace(v); text != "" {
				parts = append(parts, text)
			}
		case map[string]any:
			switch strings.ToLower(strings.TrimSpace(safeStringValue(v["type"]))) {
			case "text":
				if text := strings.TrimSpace(safeStringValue(v["text"])); text != "" {
					parts = append(parts, text)
				}
			case "tool_reference":
				name := safeStringValue(v["tool_name"])
				if name == "" {
					name = safeStringValue(v["name"])
				}
				if name == "" {
					parts = append(parts, "[Tool reference loaded]")
				} else {
					parts = append(parts, "[Tool reference loaded: "+name+"]")
				}
			case "cache_edits":
				continue
			default:
				if raw := strings.TrimSpace(formatClaudeUnknownBlockForPrompt(v)); raw != "" {
					parts = append(parts, raw)
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}

func firstClaudeBlockText(block map[string]any, keys ...string) string {
	for _, key := range keys {
		if text := strings.TrimSpace(safeStringValue(block[key])); text != "" {
			return text
		}
	}
	return ""
}

func emptyClaudeToolResultMessage(toolName string) string {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return "(tool completed with no output)"
	}
	return "(" + toolName + " completed with no output)"
}

func formatClaudeBlockRaw(block map[string]any) string {
	if block == nil {
		return ""
	}
	b, err := json.Marshal(block)
	if err != nil {
		return strings.TrimSpace(fmt.Sprintf("%v", block))
	}
	return string(b)
}
