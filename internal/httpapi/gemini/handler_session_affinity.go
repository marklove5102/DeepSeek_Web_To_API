package gemini

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func geminiSessionAffinityScope(routeModel string, req map[string]any) string {
	root := "gemini:model:" + strings.TrimSpace(routeModel)
	if root == "gemini:model:" {
		root = "gemini:caller"
	}
	if fp := geminiFirstUserFingerprint(req); fp != "" {
		return root + ":body:" + fp
	}
	return root
}

func geminiFirstUserFingerprint(req map[string]any) string {
	if req == nil {
		return ""
	}
	systemText := geminiSystemText(req["systemInstruction"])
	userText := geminiFirstUserText(req["contents"])
	if userText == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(systemText + "\x00" + userText))
	return hex.EncodeToString(sum[:8])
}

func geminiSystemText(system any) string {
	doc, ok := system.(map[string]any)
	if !ok {
		return ""
	}
	parts, ok := doc["parts"].([]any)
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, item := range parts {
		part, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if text := strings.TrimSpace(geminiString(part["text"])); text != "" {
			b.WriteString(text)
			b.WriteByte('\n')
		}
	}
	return strings.TrimSpace(b.String())
}

func geminiFirstUserText(contents any) string {
	items, ok := contents.([]any)
	if !ok {
		return ""
	}
	for _, item := range items {
		content, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := strings.TrimSpace(geminiString(content["role"]))
		if role != "" && role != "user" {
			continue
		}
		parts, ok := content["parts"].([]any)
		if !ok {
			continue
		}
		var b strings.Builder
		for _, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				continue
			}
			if text := strings.TrimSpace(geminiString(part["text"])); text != "" {
				b.WriteString(text)
				b.WriteByte('\n')
			}
		}
		return strings.TrimSpace(b.String())
	}
	return ""
}

func geminiString(v any) string {
	s, _ := v.(string)
	return s
}
