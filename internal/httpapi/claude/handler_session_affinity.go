package claude

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

func claudeSessionAffinityScope(r *http.Request, req map[string]any) string {
	root := claudeSessionAffinityRoot(r, req)
	if root == "" {
		root = "claude:caller"
	}
	if lane := claudeExplicitLaneScope(req); lane != "" {
		return root + ":lane:" + lane
	}
	if fp := claudeFirstUserFingerprint(req); fp != "" {
		return root + ":lane:body:" + fp
	}
	return root
}

func claudeSessionAffinityRoot(r *http.Request, req map[string]any) string {
	if r != nil {
		for _, header := range []string{"X-Claude-Code-Session-Id", "X-Claude-Remote-Session-Id"} {
			if v := strings.TrimSpace(r.Header.Get(header)); v != "" {
				return "claude:header:" + strings.ToLower(header) + ":" + v
			}
		}
	}
	if req == nil {
		return ""
	}
	if root := claudeDirectSessionRoot(req); root != "" {
		return root
	}
	return claudeMetadataSessionRoot(req)
}

func claudeDirectSessionRoot(req map[string]any) string {
	for _, key := range []string{"session_id", "conversation_id", "thread_id"} {
		if v := strings.TrimSpace(safeStringValue(req[key])); v != "" {
			return "claude:" + key + ":" + v
		}
	}
	return ""
}

func claudeMetadataSessionRoot(req map[string]any) string {
	meta, ok := req["metadata"].(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"session_id", "conversation_id", "thread_id"} {
		if v := strings.TrimSpace(safeStringValue(meta[key])); v != "" {
			return "claude:metadata:" + key + ":" + v
		}
	}
	userID := strings.TrimSpace(safeStringValue(meta["user_id"]))
	if userID == "" {
		userID = strings.TrimSpace(safeStringValue(meta["user"]))
	}
	if userID == "" {
		return ""
	}
	if scope := claudeMetadataUserIDScope(userID); scope != "" {
		return scope
	}
	return "claude:metadata:user_id:" + userID
}

func claudeMetadataUserIDScope(userID string) string {
	var doc map[string]any
	if err := json.Unmarshal([]byte(userID), &doc); err != nil {
		return ""
	}
	for _, field := range []struct {
		key   string
		label string
	}{
		{key: "session_id", label: "session_id"},
		{key: "sessionId", label: "session_id"},
		{key: "conversation_id", label: "conversation_id"},
		{key: "conversationId", label: "conversation_id"},
		{key: "thread_id", label: "thread_id"},
		{key: "threadId", label: "thread_id"},
	} {
		if v := strings.TrimSpace(safeStringValue(doc[field.key])); v != "" {
			return "claude:metadata:user_id." + field.label + ":" + v
		}
	}
	return ""
}

func claudeExplicitLaneScope(req map[string]any) string {
	if req == nil {
		return ""
	}
	for _, field := range claudeLaneFields() {
		if v := strings.TrimSpace(safeStringValue(req[field.key])); v != "" {
			return field.label + ":" + v
		}
	}
	return claudeMetadataLaneScope(req)
}

func claudeMetadataLaneScope(req map[string]any) string {
	meta, ok := req["metadata"].(map[string]any)
	if !ok {
		return ""
	}
	for _, field := range claudeLaneFields() {
		if v := strings.TrimSpace(safeStringValue(meta[field.key])); v != "" {
			return "metadata:" + field.label + ":" + v
		}
	}
	if userID := strings.TrimSpace(safeStringValue(meta["user_id"])); userID != "" {
		return claudeMetadataUserIDLaneScope(userID)
	}
	return ""
}

func claudeMetadataUserIDLaneScope(userID string) string {
	var doc map[string]any
	if err := json.Unmarshal([]byte(userID), &doc); err != nil {
		return ""
	}
	for _, field := range claudeLaneFields() {
		if v := strings.TrimSpace(safeStringValue(doc[field.key])); v != "" {
			return "metadata:user_id." + field.label + ":" + v
		}
	}
	return ""
}

func claudeLaneFields() []struct {
	key   string
	label string
} {
	return []struct {
		key   string
		label string
	}{
		{key: "agent_id", label: "agent_id"},
		{key: "agentId", label: "agent_id"},
		{key: "subagent_id", label: "subagent_id"},
		{key: "subagentId", label: "subagent_id"},
		{key: "sub_agent_id", label: "sub_agent_id"},
		{key: "subAgentId", label: "sub_agent_id"},
		{key: "task_id", label: "task_id"},
		{key: "taskId", label: "task_id"},
		{key: "task_uuid", label: "task_uuid"},
		{key: "taskUuid", label: "task_uuid"},
		{key: "run_id", label: "run_id"},
		{key: "runId", label: "run_id"},
	}
}

func claudeFirstUserFingerprint(req map[string]any) string {
	if req == nil {
		return ""
	}
	msgs, ok := req["messages"].([]any)
	if !ok {
		return ""
	}
	for _, item := range msgs {
		msg, ok := item.(map[string]any)
		if !ok || strings.TrimSpace(safeStringValue(msg["role"])) != "user" {
			continue
		}
		userText := claudeContentText(msg["content"])
		if userText == "" {
			return ""
		}
		systemText := claudeSystemText(req["system"])
		sum := sha256.Sum256([]byte(systemText + "\x00" + userText))
		return hex.EncodeToString(sum[:8])
	}
	return ""
}

func claudeSystemText(system any) string {
	switch v := system.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		return strings.TrimSpace(safeStringValue(v["text"]))
	case []any:
		var b strings.Builder
		for _, item := range v {
			switch block := item.(type) {
			case string:
				if text := strings.TrimSpace(block); text != "" {
					b.WriteString(text)
					b.WriteByte('\n')
				}
				continue
			case map[string]any:
				if text := strings.TrimSpace(safeStringValue(block["text"])); text != "" {
					b.WriteString(text)
					b.WriteByte('\n')
				}
			}
		}
		return strings.TrimSpace(b.String())
	default:
		return ""
	}
}

func claudeContentText(content any) string {
	switch v := content.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		return claudeContentBlocksText(v)
	default:
		return ""
	}
}

func claudeContentBlocksText(blocks []any) string {
	var b strings.Builder
	for _, item := range blocks {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if text := strings.TrimSpace(safeStringValue(block["text"])); text != "" {
			b.WriteString(text)
			b.WriteByte('\n')
			continue
		}
		if text := strings.TrimSpace(safeStringValue(block["content"])); text != "" {
			b.WriteString(text)
			b.WriteByte('\n')
		}
	}
	return strings.TrimSpace(b.String())
}
