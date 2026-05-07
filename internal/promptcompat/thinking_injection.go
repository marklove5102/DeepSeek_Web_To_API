package promptcompat

import "strings"

// The historical injection wrote a ~3 KB block of "reasoning effort + tool
// chain discipline + 5 workflow patterns + MCP rules + stopping criteria"
// to the *latest user message*. That had two failure modes:
//
//  1. The DSML format/RULES half was already injected into the *system*
//     message by injectToolPrompt → BuildToolCallInstructions, so each
//     request carried two near-duplicate rule sets and the user prompt
//     itself got pushed down by ~3 KB.
//  2. When upstream took the no-thinking fast path (returns empty
//     reasoning_content even when thinking_enabled=true), the user-tail
//     injection was read but never "deliberated", so the workflow
//     patterns were silently dropped — the model would then emit prose
//     directly instead of a tool_use block (Issue #18).
//
// The fix below splits the injection: the short "reason hard" half stays at
// the user-message tail (its job is per-turn motivation), and the longer
// workflow/orchestration half — which is stable across turns and survives
// fast-path — moves into the system message alongside BuildToolCallInstructions.
const (
	ThinkingInjectionMarker = "Reasoning Effort: Absolute maximum with no shortcuts permitted."

	// ReasoningEffortPrompt is short and per-turn. Appended to the latest
	// user message. It exists to fight the upstream fast-path: even when
	// thinking_enabled=true is honored, the model sometimes elects to skip
	// reasoning. This block reminds it not to.
	ReasoningEffortPrompt = ThinkingInjectionMarker + "\n" +
		"Decompose the problem before answering. Stress-test your logic against edge cases and adversarial inputs. " +
		"If a tool call is the right action, emit it now — do not narrate the plan and stop. " +
		"If you are not going to call a tool, say so explicitly and produce the final answer."

	// ToolChainPlaybookPrompt is the long, stable orchestration playbook.
	// Promoted to the system message (next to the DSML format RULES) so it
	// survives fast-path turns and is not duplicated against system-level
	// tool format rules.
	ToolChainPlaybookPrompt = "Tool-Chain Discipline (read before every tool decision):\n" +
		"1. CALL a tool only when you need information you do not have or an action on an external resource. Never guess a value you could read.\n" +
		"2. PARALLEL vs SEQUENTIAL — when multiple calls are independent, emit them inside the SAME <|DSML|tool_calls> block so they run concurrently. Only chain calls when one depends on another's output.\n" +
		"3. AFTER A RESULT — read it carefully, then either chain a follow-up call OR produce the final answer. Diagnose tool errors at the root cause; do NOT blindly retry the same call with the same arguments. Two failures of the same call halts the chain.\n" +
		"4. STOP — terminate as soon as the user's request is satisfied. Do not invoke extra tools as filler. The final response is prose addressed to the user, NOT another tool-calls block.\n" +
		"\n" +
		"Tool-Chain Patterns (the shapes of every chain you will run):\n" +
		"\n" +
		"A. READ-BEFORE-EDIT — file modification requires Read first, then Edit using EXACT bytes from the Read result. Never paste a remembered snippet into old_string.\n" +
		"B. SEARCH → NARROW → INSPECT — Glob+Grep in ONE parallel block, pick ≤5 files, Read them in ONE parallel block, then synthesize. Do not loop back to Glob/Grep unless step 3 surfaced a genuinely new keyword.\n" +
		"C. BASH + DIAGNOSIS — run, then check exit code. exit≠0: read stderr, name the actual cause (missing flag? wrong path? permission?) before any retry. A retry with identical args is forbidden.\n" +
		"D. PARALLEL RESEARCH — WebSearch / WebFetch / Grep / Read in ONE block when independent. Re-fetching the same URL or re-reading the same file is a wasted call.\n" +
		"E. CONDITIONAL FOLLOW-UP — if call B's input depends on call A's output, emit B in a SEPARATE turn AFTER reading A's result.\n" +
		"\n" +
		"MCP Tool Invocation:\n" +
		"- MCP tools advertise as `<server>.<tool>` (dotted namespace). Invoke EXACTLY by that name. Single-word `<tool>` will not route.\n" +
		"- Parameter names come from the schema in the tool list — never invent them.\n" +
		"- Server names not in the tool schema are hallucinations. If the user asks for a server you have not seen, say so plainly instead of inventing the call.\n" +
		"\n" +
		"Stopping Criteria — exit at the FIRST of:\n" +
		"- The user's question is fully answered with the data already gathered.\n" +
		"- ≥3 consecutive tool calls have produced no progress on the same sub-problem (you are stuck — stop and explain).\n" +
		"- The same tool with the same arguments has failed twice (the third attempt is forbidden).\n" +
		"- You can predict the next tool's output from prior context (don't burn a roundtrip on a foregone conclusion).\n" +
		"After stopping, produce ONE final user-facing prose response. Never alternate prose and tool-calls within a single emission."

	// DefaultThinkingInjectionPrompt is the legacy concatenation of both
	// halves. Preserved verbatim so callers passing it explicitly (and the
	// existing test contracts) continue to work, but new code paths should
	// use ReasoningEffortPrompt + ToolChainPlaybookPrompt with a system /
	// user split via AppendThinkingInjectionPromptToLatestUser.
	DefaultThinkingInjectionPrompt = ReasoningEffortPrompt + "\n\n" + ToolChainPlaybookPrompt
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

// PrependPlaybookToSystem inserts the tool-chain playbook at the head of the
// first system message, or creates a new leading system message when none
// exists. The playbook is only added when the request actually carries tools
// — without tools the playbook is dead weight.
//
// Returns the (possibly mutated) message slice and whether it changed.
// Idempotent: a second call with the same playbook string is a no-op.
func PrependPlaybookToSystem(messages []any, playbook string) ([]any, bool) {
	playbook = strings.TrimSpace(playbook)
	if playbook == "" {
		return messages, false
	}
	for i, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(asString(msg["role"])))
		if role != "system" {
			continue
		}
		existing := NormalizeOpenAIContentForPrompt(msg["content"])
		if strings.Contains(existing, playbook) {
			return messages, false
		}
		out := append([]any(nil), messages...)
		cloned := make(map[string]any, len(msg))
		for k, v := range msg {
			cloned[k] = v
		}
		cloned["content"] = playbook + "\n\n" + existing
		out[i] = cloned
		return out, true
	}
	// No system message exists — insert one at the head.
	out := make([]any, 0, len(messages)+1)
	out = append(out, map[string]any{"role": "system", "content": playbook})
	out = append(out, messages...)
	return out, true
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
