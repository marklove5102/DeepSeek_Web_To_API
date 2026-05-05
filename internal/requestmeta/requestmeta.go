package requestmeta

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
)

var ConversationIDHeaders = []string{
	"X-DeepSeek-Web-To-API-Conversation-ID",
	"X-Ds2-Conversation-ID",
	"X-Conversation-ID",
	"Conversation-ID",
	"X-Codex-Conversation-ID",
	"X-Codex-Session-ID",
	"X-OpenCode-Conversation-ID",
	"X-OpenCode-Session-ID",
	"OpenAI-Conversation-ID",
	"Anthropic-Conversation-ID",
}

var conversationIDBodyKeys = map[string]struct{}{
	"conversation_id": {},
	"conversationid":  {},
	"chat_id":         {},
	"chatid":          {},
	"thread_id":       {},
	"threadid":        {},
	"session_id":      {},
	"sessionid":       {},
	"parent_id":       {},
	"parentid":        {},
}

type Metadata struct {
	ClientIP       string
	ConversationID string
}

func ClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	for _, header := range []string{"X-Real-IP", "CF-Connecting-IP", "X-Forwarded-For"} {
		for _, value := range r.Header.Values(header) {
			if header == "X-Forwarded-For" {
				for _, part := range strings.Split(value, ",") {
					if ip := cleanIP(part); ip != "" {
						return ip
					}
				}
				continue
			}
			if ip := cleanIP(value); ip != "" {
				return ip
			}
		}
	}
	if ip := cleanIP(r.RemoteAddr); ip != "" {
		return ip
	}
	return ""
}

func ConversationID(r *http.Request, body []byte) string {
	if r != nil {
		for _, header := range ConversationIDHeaders {
			if id := normalizeID(r.Header.Get(header)); id != "" {
				return id
			}
		}
	}
	if len(body) == 0 {
		return ""
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return ""
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return ""
	}
	return conversationIDFromJSON(value, 0)
}

func cleanIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	raw = strings.Trim(raw, "[]")
	ip := net.ParseIP(raw)
	if ip == nil {
		return ""
	}
	return ip.String()
}

func conversationIDFromJSON(value any, depth int) string {
	if depth > 3 {
		return ""
	}
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if _, ok := conversationIDBodyKeys[canonicalIDKey(key)]; ok {
				if id := normalizeID(item); id != "" {
					return id
				}
			}
		}
		for _, key := range []string{"metadata", "conversation", "session", "thread"} {
			if nested, ok := v[key]; ok {
				if id := conversationIDFromJSON(nested, depth+1); id != "" {
					return id
				}
			}
		}
	}
	return ""
}

func canonicalIDKey(key string) string {
	key = strings.TrimSpace(key)
	key = strings.ReplaceAll(key, "-", "_")
	key = strings.ReplaceAll(key, " ", "_")
	return strings.ToLower(key)
}

func normalizeID(value any) string {
	var raw string
	switch v := value.(type) {
	case string:
		raw = v
	case json.Number:
		raw = v.String()
	default:
		return ""
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 256 {
		return ""
	}
	for _, r := range raw {
		if r < 32 || r == 127 {
			return ""
		}
	}
	return raw
}
