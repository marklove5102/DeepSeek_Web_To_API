package promptcompat

import (
	"fmt"
	"strings"

	"DeepSeek_Web_To_API/internal/prompt"
)

type pendingToolCall struct {
	ID   string
	Name string
}

type toolMessageRepairState struct {
	pending []pendingToolCall
	nextID  int
}

func repairOpenAIToolMessages(raw []any) []any {
	if len(raw) == 0 {
		return raw
	}
	state := &toolMessageRepairState{}
	out := make([]any, 0, len(raw))
	for _, item := range raw {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		repaired := repairOpenAIToolMessage(msg, state)
		out = append(out, repaired...)
	}
	return out
}

func repairOpenAIToolMessage(msg map[string]any, state *toolMessageRepairState) []any {
	role := strings.ToLower(strings.TrimSpace(asString(msg["role"])))
	itemType := strings.ToLower(strings.TrimSpace(asString(msg["type"])))

	if role == "" {
		switch itemType {
		case "function_call_output", "tool_result", "tooloutput", "tool_output":
			return []any{state.repairToolResultMessage(msg)}
		case "function_call", "tool_call", "tool_use", "toolcall", "tool_use_call":
			if assistant := state.assistantMessageFromToolCallItem(msg); assistant != nil {
				return []any{assistant}
			}
		}
	}

	switch role {
	case "assistant":
		if split := state.repairAssistantContentBlocks(msg); len(split) > 0 {
			return split
		}
		copied := clonePromptCompatMap(msg)
		if calls := state.repairToolCalls(copied["tool_calls"]); len(calls) > 0 {
			copied["tool_calls"] = calls
		}
		return []any{copied}
	case "tool", "function":
		return []any{state.repairToolResultMessage(msg)}
	case "user":
		if hasToolResultShape(msg) {
			return []any{state.repairToolResultMessage(msg)}
		}
		if split := state.repairToolResultsFromContent(role, msg["content"]); len(split) > 0 {
			return split
		}
	default:
		if split := state.repairToolResultsFromContent(role, msg["content"]); len(split) > 0 {
			return split
		}
	}

	return []any{clonePromptCompatMap(msg)}
}

func (s *toolMessageRepairState) repairAssistantContentBlocks(msg map[string]any) []any {
	items, ok := msg["content"].([]any)
	if !ok || len(items) == 0 {
		return nil
	}

	textParts := make([]string, 0, len(items))
	toolCalls := make([]any, 0, len(items))
	for _, item := range items {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		blockType := strings.ToLower(strings.TrimSpace(asString(block["type"])))
		switch blockType {
		case "tool_use", "function_call", "tool_call", "toolcall", "tool_use_call":
			if call := s.repairToolCall(block); call != nil {
				toolCalls = append(toolCalls, call)
			}
		case "text", "input_text", "output_text":
			if text := textFromContentBlock(block); strings.TrimSpace(text) != "" {
				textParts = append(textParts, text)
			}
		default:
			if text := strings.TrimSpace(NormalizeOpenAIContentForPrompt([]any{block})); text != "" {
				textParts = append(textParts, text)
			}
		}
	}
	if len(toolCalls) == 0 {
		return nil
	}

	out := clonePromptCompatMap(msg)
	delete(out, "content")
	if len(textParts) > 0 {
		out["content"] = strings.Join(textParts, "\n")
	}
	out["tool_calls"] = s.recordPendingToolCalls(toolCalls)
	return []any{out}
}

func (s *toolMessageRepairState) repairToolResultsFromContent(role string, content any) []any {
	normalizedRole := normalizeOpenAIRoleForPrompt(role)
	items, ok := content.([]any)
	if !ok || len(items) == 0 {
		if block, ok := content.(map[string]any); ok && isToolResultBlock(block) {
			return []any{s.repairToolResultMessage(block)}
		}
		return nil
	}

	out := make([]any, 0, len(items))
	textParts := make([]string, 0, len(items))
	flushText := func() {
		if len(textParts) == 0 {
			return
		}
		roleForText := normalizedRole
		if strings.TrimSpace(roleForText) == "" {
			roleForText = "user"
		}
		out = append(out, map[string]any{
			"role":    roleForText,
			"content": strings.Join(textParts, "\n"),
		})
		textParts = textParts[:0]
	}

	foundToolResult := false
	for _, item := range items {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if isToolResultBlock(block) {
			foundToolResult = true
			flushText()
			out = append(out, s.repairToolResultMessage(block))
			continue
		}
		if text := textFromContentBlock(block); strings.TrimSpace(text) != "" {
			textParts = append(textParts, text)
		}
	}
	flushText()
	if !foundToolResult {
		return nil
	}
	return out
}

func (s *toolMessageRepairState) assistantMessageFromToolCallItem(item map[string]any) map[string]any {
	call := s.repairToolCall(item)
	if call == nil {
		return nil
	}
	return map[string]any{
		"role":       "assistant",
		"tool_calls": s.recordPendingToolCalls([]any{call}),
	}
}

func (s *toolMessageRepairState) repairToolCalls(raw any) []any {
	var items []any
	switch v := raw.(type) {
	case []any:
		items = v
	case map[string]any:
		items = []any{v}
	default:
		return nil
	}
	calls := make([]any, 0, len(items))
	for _, item := range items {
		call, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if repaired := s.repairToolCall(call); repaired != nil {
			calls = append(calls, repaired)
		}
	}
	return s.recordPendingToolCalls(calls)
}

func (s *toolMessageRepairState) repairToolCall(call map[string]any) map[string]any {
	if call == nil {
		return nil
	}
	fn, _ := call["function"].(map[string]any)
	name := firstNonEmptyString(
		call["name"],
		call["tool_name"],
		call["function_name"],
		call["recipient_name"],
		call["toolName"],
		call["functionName"],
	)
	if name == "" && fn != nil {
		name = firstNonEmptyString(fn["name"], fn["tool_name"], fn["function_name"], fn["toolName"], fn["functionName"])
	}
	if name == "" {
		return nil
	}

	argsRaw := firstExisting(call, "arguments", "input", "args", "parameters", "params", "arguments_json", "input_json")
	if argsRaw == nil && fn != nil {
		argsRaw = firstExisting(fn, "arguments", "input", "args", "parameters", "params", "arguments_json", "input_json")
	}

	id := firstNonEmptyString(call["id"], call["call_id"], call["tool_call_id"], call["tool_use_id"], call["callId"], call["toolCallId"], call["toolUseId"])
	if id == "" {
		id = s.nextToolCallID()
	}

	return map[string]any{
		"id":   id,
		"type": "function",
		"function": map[string]any{
			"name":      name,
			"arguments": prompt.StringifyToolCallArguments(argsRaw),
		},
	}
}

func (s *toolMessageRepairState) repairToolResultMessage(msg map[string]any) map[string]any {
	content := firstExisting(msg, "content", "output", "result", "text", "observation", "data")
	if block, ok := content.(map[string]any); ok && isToolResultBlock(block) {
		content = firstExisting(block, "content", "output", "result", "text", "observation", "data")
	}
	if content == nil {
		content = ""
	}

	name := firstNonEmptyString(msg["name"], msg["tool_name"], msg["function_name"], msg["recipient_name"], msg["toolName"], msg["functionName"])
	callID := firstNonEmptyString(msg["tool_call_id"], msg["call_id"], msg["tool_use_id"], msg["id"], msg["callId"], msg["toolCallId"], msg["toolUseId"])
	callID, name = s.consumePendingToolCall(callID, name)

	out := map[string]any{
		"role":         "tool",
		"tool_call_id": callID,
		"content":      content,
	}
	if name != "" {
		out["name"] = name
	}
	return out
}

func (s *toolMessageRepairState) recordPendingToolCalls(calls []any) []any {
	if len(calls) == 0 {
		return nil
	}
	for _, item := range calls {
		call, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := firstNonEmptyString(call["id"], call["call_id"], call["tool_call_id"], call["callId"], call["toolCallId"])
		if id == "" {
			continue
		}
		name := firstNonEmptyString(call["name"])
		if fn, ok := call["function"].(map[string]any); ok && name == "" {
			name = firstNonEmptyString(fn["name"])
		}
		s.pending = append(s.pending, pendingToolCall{ID: id, Name: name})
	}
	return calls
}

func (s *toolMessageRepairState) consumePendingToolCall(callID, name string) (string, string) {
	callID = strings.TrimSpace(callID)
	name = strings.TrimSpace(name)
	if callID != "" {
		for i, pending := range s.pending {
			if pending.ID == callID {
				s.pending = removePendingToolCall(s.pending, i)
				if name == "" {
					name = pending.Name
				}
				return callID, name
			}
		}
	}
	if name != "" {
		for i, pending := range s.pending {
			if strings.EqualFold(pending.Name, name) {
				s.pending = removePendingToolCall(s.pending, i)
				return pending.ID, name
			}
		}
	}
	if len(s.pending) == 1 || callID == "" {
		if len(s.pending) > 0 {
			pending := s.pending[0]
			s.pending = removePendingToolCall(s.pending, 0)
			if name == "" {
				name = pending.Name
			}
			return pending.ID, name
		}
	}
	if callID == "" {
		callID = s.nextToolCallID()
	}
	return callID, name
}

func (s *toolMessageRepairState) nextToolCallID() string {
	s.nextID++
	return fmt.Sprintf("call_repaired_%d", s.nextID)
}

func removePendingToolCall(pending []pendingToolCall, index int) []pendingToolCall {
	if index < 0 || index >= len(pending) {
		return pending
	}
	copy(pending[index:], pending[index+1:])
	return pending[:len(pending)-1]
}

func hasToolResultShape(msg map[string]any) bool {
	if msg == nil {
		return false
	}
	if firstNonEmptyString(msg["tool_call_id"], msg["call_id"], msg["tool_use_id"], msg["callId"], msg["toolCallId"], msg["toolUseId"]) != "" {
		return true
	}
	if isToolResultBlock(msg) {
		return true
	}
	if block, ok := msg["content"].(map[string]any); ok && isToolResultBlock(block) {
		return true
	}
	return false
}

func isToolResultBlock(block map[string]any) bool {
	if block == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(asString(block["type"]))) {
	case "tool_result", "function_call_output", "tooloutput", "tool_output":
		return true
	default:
		return false
	}
}

func textFromContentBlock(block map[string]any) string {
	if block == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(asString(block["type"]))) {
	case "text", "input_text", "output_text":
		return firstNonEmptyString(block["text"], block["content"])
	default:
		return ""
	}
}

func firstExisting(m map[string]any, keys ...string) any {
	if m == nil {
		return nil
	}
	for _, key := range keys {
		if value, ok := m[key]; ok && value != nil {
			return value
		}
	}
	return nil
}

func firstNonEmptyString(values ...any) string {
	for _, value := range values {
		if text := strings.TrimSpace(asString(value)); text != "" {
			return text
		}
	}
	return ""
}

func clonePromptCompatMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
