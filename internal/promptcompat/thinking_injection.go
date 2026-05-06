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
		"6. FAILURE MODES TO AVOID — wrapping tool-call XML inside markdown code fences; mixing prose and tool-call markup in the same emission; inventing tool names or parameter names absent from the schema; passing the wrong parameter shape (string where object is expected, etc.); silently swallowing a tool error and proceeding as if it succeeded.\n" +
		"\n" +
		"Tool-Chain Patterns — concrete multi-step workflows. Memorize these; they are the shapes of every tool chain you will run:\n" +
		"\n" +
		"A. READ-BEFORE-EDIT (file modification — required order):\n" +
		"   1) <|DSML|invoke name=\"Read\"><|DSML|parameter name=\"file_path\"><![CDATA[/abs/path]]></|DSML|parameter></|DSML|invoke>\n" +
		"   2) WAIT for the result; copy the EXACT bytes you want to replace into old_string.\n" +
		"   3) <|DSML|invoke name=\"Edit\"><|DSML|parameter name=\"file_path\">…</|DSML|parameter><|DSML|parameter name=\"old_string\"><![CDATA[exact text from step 2]]></|DSML|parameter><|DSML|parameter name=\"new_string\"><![CDATA[replacement]]></|DSML|parameter></|DSML|invoke>\n" +
		"   Edit will refuse the call if Read has not run first. Never paste a remembered or guessed snippet into old_string — it must come from the Read result.\n" +
		"\n" +
		"B. SEARCH → NARROW → INSPECT (codebase exploration):\n" +
		"   1) Independent search calls in ONE block (parallel): Glob (file pattern) + Grep (text search).\n" +
		"   2) WAIT for both. Pick the most relevant N files (target N≤5).\n" +
		"   3) Independent Read calls for those N files in ONE block (parallel).\n" +
		"   4) Synthesize a final prose answer. Do NOT loop back to Glob/Grep unless step 3 surfaced a genuinely new keyword.\n" +
		"\n" +
		"C. BASH COMMAND + RESULT DIAGNOSIS:\n" +
		"   1) <|DSML|invoke name=\"Bash\"><|DSML|parameter name=\"command\"><![CDATA[…]]></|DSML|parameter><|DSML|parameter name=\"description\"><![CDATA[one-line purpose]]></|DSML|parameter></|DSML|invoke>\n" +
		"   2) WAIT for stdout/stderr/exit. If exit==0, proceed. If exit≠0, READ stderr carefully — diagnose the actual cause (missing flag? wrong path? permission?) before any retry. A retry with identical args is forbidden; a retry with adjusted args must be preceded by an explicit reasoning step naming the change.\n" +
		"\n" +
		"D. PARALLEL RESEARCH (web + code, mixed sources):\n" +
		"   Emit WebSearch / WebFetch / Grep / Read in ONE <|DSML|tool_calls> block when none of them depend on each other's output. Re-fetching the same URL or re-reading the same file in a follow-up turn is a wasted call.\n" +
		"\n" +
		"E. CONDITIONAL FOLLOW-UP (depends on prior result):\n" +
		"   If call B's input is derived from call A's output (e.g., Edit needs a snippet from Read; Bash test command needs the file path returned by Glob), emit B in a SEPARATE turn AFTER reading A's result. Do not pre-guess A's output and emit A+B together.\n" +
		"\n" +
		"MCP-Tool Invocation — Anthropic mcp_servers protocol bridging:\n" +
		"- MCP-server tools advertise as `<server>.<tool>` (dotted namespace). Invoke EXACTLY by that name. Single-word `<tool>` will not route to the MCP server.\n" +
		"  Example for server `weather` exposing tool `get_forecast`:\n" +
		"  <|DSML|invoke name=\"weather.get_forecast\">\n" +
		"    <|DSML|parameter name=\"city\"><![CDATA[Beijing]]></|DSML|parameter>\n" +
		"  </|DSML|invoke>\n" +
		"- Parameter names come from the MCP server's input_schema declared in the tool list. Inspect the schema in the system tool definitions before invoking; do NOT invent parameter names.\n" +
		"- MCP results return as ordinary tool results — read content carefully, then either chain another call (MCP or regular) or produce the final answer.\n" +
		"- Server names you have NOT seen in the tool schema are hallucinations. If the user asks for `linear.create_issue` but no `linear` server is advertised, say so plainly instead of inventing the call.\n" +
		"- Mixing a regular tool (`Read`, `Bash`) and an MCP tool (`weather.get_forecast`) in the same parallel <|DSML|tool_calls> block is allowed when their inputs are independent.\n" +
		"\n" +
		"Stopping Criteria — exit the tool chain at the FIRST of:\n" +
		"- The user's question is fully answered with the data already gathered.\n" +
		"- ≥3 consecutive tool calls have produced no progress on the same sub-problem (you are stuck — stop and explain rather than thrashing).\n" +
		"- The same tool with the same arguments has failed twice (the third attempt is forbidden; diagnose and explain).\n" +
		"- You can predict the next tool's output from prior context (don't burn a roundtrip on a foregone conclusion).\n" +
		"After stopping, produce ONE final user-facing prose response. Never alternate prose and tool-calls within a single emission."
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
