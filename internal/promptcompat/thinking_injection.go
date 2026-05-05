package promptcompat

import "strings"

const (
	ThinkingInjectionMarker        = "Reasoning Effort: Absolute maximum with no shortcuts permitted."
	DefaultThinkingInjectionPrompt = ThinkingInjectionMarker + "\n" +
		"You MUST be very thorough in your thinking and comprehensively decompose the problem to resolve the root cause, rigorously stress-testing your logic against all potential paths, edge cases, and adversarial scenarios.\n" +
		"Explicitly write out your entire deliberation process, documenting every intermediate step, considered alternative, and rejected hypothesis to ensure absolutely no assumption is left unchecked.\n" +
		"\n" +
		"Tool-Chain Discipline — read this before every tool decision:\n" +
		"1. CALL a tool only when you need information you do not have or an action on an external resource (file, command, API, search). Never guess a value you could read.\n" +
		"2. FORMAT — strict, no improvisation. Every tool call uses the canonical wrapper:\n" +
		"   <|DSML|tool_calls>\n" +
		"     <|DSML|invoke name=\"TOOL_NAME\">\n" +
		"       <|DSML|parameter name=\"ARG\"><![CDATA[VALUE]]></|DSML|parameter>\n" +
		"     </|DSML|invoke>\n" +
		"   </|DSML|tool_calls>\n" +
		"   The wrapper tags use pipes ('<|DSML|...|>') but CDATA uses SQUARE BRACKETS ('<![CDATA[' and ']]>'). Mixing the two — emitting '<![CDATA|VALUE|]]>' or '<![CDATA|VALUE]]>' — is INVALID; the wrapper will leak into the parameter value and surface verbatim in the client UI.\n" +
		"3. PARALLEL vs SEQUENTIAL — when multiple calls are independent (no call's input depends on another's output) emit them inside the SAME <|DSML|tool_calls> block so they run concurrently. When a call needs a prior call's result, emit the dependent call only AFTER reading that result.\n" +
		"4. AFTER A RESULT — read it carefully, then choose ONE: (a) chain a follow-up tool call, or (b) produce the final answer. Diagnose tool errors at the root cause; do NOT blindly retry the same call with the same arguments. If the same call has failed twice, halt the chain and explain, do not attempt a third identical retry.\n" +
		"5. STOP — terminate the tool chain as soon as the user's request is fully satisfied. Do not invoke extra tools as filler, do not loop. The final response must be prose addressed to the user, NOT another tool-calls block.\n" +
		"6. FAILURE MODES TO AVOID — wrapping tool-call XML inside markdown code fences; mixing prose and tool-call markup in the same emission; inventing tool names or parameter names absent from the schema; passing the wrong parameter shape (string where object is expected, etc.); silently swallowing a tool error and proceeding as if it succeeded."
)

func AppendThinkingInjectionToLatestUser(messages []any) ([]any, bool) {
	return AppendThinkingInjectionPromptToLatestUser(messages, "")
}

func AppendThinkingInjectionPromptToLatestUser(messages []any, injectionPrompt string) ([]any, bool) {
	if len(messages) == 0 {
		return messages, false
	}
	injectionPrompt = strings.TrimSpace(injectionPrompt)
	if injectionPrompt == "" {
		injectionPrompt = DefaultThinkingInjectionPrompt
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		if strings.ToLower(strings.TrimSpace(asString(msg["role"]))) != "user" {
			continue
		}
		content := msg["content"]
		normalizedContent := NormalizeOpenAIContentForPrompt(content)
		if strings.Contains(normalizedContent, ThinkingInjectionMarker) || strings.Contains(normalizedContent, injectionPrompt) {
			return messages, false
		}
		updatedContent := appendThinkingInjectionToContent(content, injectionPrompt)
		out := append([]any(nil), messages...)
		cloned := make(map[string]any, len(msg))
		for k, v := range msg {
			cloned[k] = v
		}
		cloned["content"] = updatedContent
		out[i] = cloned
		return out, true
	}
	return messages, false
}

func appendThinkingInjectionToContent(content any, injectionPrompt string) any {
	switch x := content.(type) {
	case string:
		return appendTextBlock(x, injectionPrompt)
	case []any:
		out := append([]any(nil), x...)
		out = append(out, map[string]any{
			"type": "text",
			"text": injectionPrompt,
		})
		return out
	default:
		text := NormalizeOpenAIContentForPrompt(content)
		return appendTextBlock(text, injectionPrompt)
	}
}

func appendTextBlock(base, addition string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return addition
	}
	return base + "\n\n" + addition
}
