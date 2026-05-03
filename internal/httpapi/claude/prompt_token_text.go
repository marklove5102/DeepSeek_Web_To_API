package claude

import "DeepSeek_Web_To_API/internal/prompt"

func buildClaudePromptTokenText(messages []any, thinkingEnabled bool) string {
	return prompt.MessagesPrepareWithThinking(toMessageMaps(messages), thinkingEnabled)
}
