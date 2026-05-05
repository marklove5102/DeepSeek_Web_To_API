package toolcall

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"
)

var toolCallMarkupKVPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?([a-z0-9_\-.]+)\b[^>]*>(.*?)</(?:[a-z0-9_:-]+:)?([a-z0-9_\-.]+)>`)

// cdataPattern matches a canonical standalone CDATA section.
var cdataPattern = regexp.MustCompile(`(?is)^<!\[CDATA\[(.*?)]]>$`)

// cdataPipeVariantPattern matches a near-miss where the model substituted the
// canonical '[' opener with a DSML-style pipe ('|' / '｜'). The trailing pipe
// before the canonical "]]>" close is optional. Real-world emission example:
// "<![CDATA|general-purpose]]>" — produced by DeepSeek when the model bleeds
// the surrounding "<|DSML|...|>" pipe convention into the CDATA opener.
var cdataPipeVariantPattern = regexp.MustCompile(`(?is)^<!\[CDATA[\|｜](.*?)[\|｜]?]]>$`)

func parseMarkupKVObject(text string) map[string]any {
	matches := toolCallMarkupKVPattern.FindAllStringSubmatch(strings.TrimSpace(text), -1)
	if len(matches) == 0 {
		return nil
	}
	out := map[string]any{}
	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		key := strings.TrimSpace(m[1])
		endKey := strings.TrimSpace(m[3])
		if key == "" {
			continue
		}
		if !strings.EqualFold(key, endKey) {
			continue
		}
		value := parseMarkupValue(m[2])
		if value == nil {
			continue
		}
		appendMarkupValue(out, key, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseMarkupValue(inner string) any {
	if value, ok := extractStandaloneCDATA(inner); ok {
		return value
	}
	value := strings.TrimSpace(extractRawTagValue(inner))
	if value == "" {
		return ""
	}

	if strings.Contains(value, "<") && strings.Contains(value, ">") {
		if parsed := parseStructuredToolCallInput(value); len(parsed) > 0 {
			if len(parsed) == 1 {
				if raw, ok := parsed["_raw"].(string); ok {
					return raw
				}
			}
			return parsed
		}
	}

	var jsonValue any
	if json.Unmarshal([]byte(value), &jsonValue) == nil {
		return jsonValue
	}
	return value
}

func appendMarkupValue(out map[string]any, key string, value any) {
	if existing, ok := out[key]; ok {
		switch current := existing.(type) {
		case []any:
			out[key] = append(current, value)
		default:
			out[key] = []any{current, value}
		}
		return
	}
	out[key] = value
}

// extractRawTagValue treats the inner content of a tag robustly.
// It detects CDATA and strips it, otherwise it unescapes standard HTML entities.
// It avoids over-aggressive tag stripping that might break user content.
func extractRawTagValue(inner string) string {
	trimmed := strings.TrimSpace(inner)
	if trimmed == "" {
		return ""
	}

	// 1. Check for CDATA - if present, it's the ultimate "safe" container.
	if value, ok := extractStandaloneCDATA(trimmed); ok {
		return value // Return raw content between CDATA brackets
	}

	// 2. If no CDATA, we still want to be robust.
	// We unescape standard HTML entities (like &lt; &gt; &amp;)
	// but we DON'T recursively strip tags unless they are actually valid XML tags
	// at the start/end (which should have been handled by the outer matcher anyway).

	// If it contains what looks like a single tag and no other text, it might be nested XML
	// but for KV objects we usually want the value.
	return html.UnescapeString(inner)
}

func extractStandaloneCDATA(inner string) (string, bool) {
	trimmed := strings.TrimSpace(inner)
	if cdataMatches := cdataPattern.FindStringSubmatch(trimmed); len(cdataMatches) >= 2 {
		return cdataMatches[1], true
	}
	if cdataMatches := cdataPipeVariantPattern.FindStringSubmatch(trimmed); len(cdataMatches) >= 2 {
		return cdataMatches[1], true
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "<![cdata[") {
		return trimmed[len("<![CDATA["):], true
	}
	if openLen := cdataPipeOpenerByteLen(trimmed); openLen > 0 {
		return trimmed[openLen:], true
	}
	return "", false
}

// cdataPipeOpenerByteLen returns the byte length of a pipe-variant CDATA opener
// at the start of trimmed (case-insensitive on the literal "<![CDATA" prefix),
// or 0 if no such opener is present. Accepts ASCII '|' (1 byte) and full-width
// '｜' (U+FF5C, 3 bytes in UTF-8) immediately after "<![CDATA".
func cdataPipeOpenerByteLen(trimmed string) int {
	const ascii = "<![CDATA|"
	const wide = "<![CDATA｜"
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, ascii) {
		return len(ascii)
	}
	if strings.HasPrefix(lower, wide) {
		return len(wide)
	}
	return 0
}

// cdataOpenerByteLenAt returns the byte length of any accepted CDATA opener
// starting exactly at position i within lower (already lowercased). Accepts
// canonical "<![CDATA[" and pipe-variant "<![CDATA|" / "<![CDATA｜". Returns 0
// when no opener begins at i.
func cdataOpenerByteLenAt(lower string, i int) int {
	if i < 0 || i >= len(lower) {
		return 0
	}
	const canonical = "<![cdata["
	const ascii = "<![cdata|"
	const wide = "<![cdata｜"
	switch {
	case strings.HasPrefix(lower[i:], canonical):
		return len(canonical)
	case strings.HasPrefix(lower[i:], ascii):
		return len(ascii)
	case strings.HasPrefix(lower[i:], wide):
		return len(wide)
	}
	return 0
}

func parseJSONLiteralValue(raw string) (any, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, false
	}

	switch trimmed[0] {
	case '{', '[', '"', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 't', 'f', 'n':
	default:
		return nil, false
	}

	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return nil, false
	}
	return parsed, true
}

// SanitizeLooseCDATA repairs malformed trailing CDATA openings just enough for
// final parsing and flush-time recovery. Properly closed CDATA blocks are left
// untouched; an unclosed opener is stripped so the remaining text can still be
// parsed as part of the surrounding tool markup.
func SanitizeLooseCDATA(text string) string {
	if text == "" {
		return ""
	}

	lower := strings.ToLower(text)
	const openMarker = "<![cdata["
	const closeMarker = "]]>"

	var b strings.Builder
	b.Grow(len(text))
	changed := false
	pos := 0
	for pos < len(text) {
		startRel := strings.Index(lower[pos:], openMarker)
		if startRel < 0 {
			b.WriteString(text[pos:])
			break
		}
		start := pos + startRel
		contentStart := start + len(openMarker)
		b.WriteString(text[pos:start])

		properRel := strings.Index(text[contentStart:], closeMarker)
		looseRel := findLooseCDATAClose(text, contentStart)

		// Pick the earliest close. "]]>" wins on tie since it's the canonical form.
		closePos := -1
		loose := false
		switch {
		case properRel >= 0 && (looseRel < 0 || properRel <= looseRel):
			closePos = contentStart + properRel
		case looseRel >= 0:
			closePos = contentStart + looseRel
			loose = true
		}

		switch {
		case closePos < 0:
			// No close marker at all — strip the opener so the rest can still parse.
			changed = true
			b.WriteString(text[contentStart:])
			pos = len(text)
		case loose:
			// Model emitted "]]<TAG" instead of "]]><TAG". Reproduce the
			// opener + content + "]]" then synthesize the missing ">".
			// "<TAG" at pos+2 is left for the next loop iteration to handle
			// as a regular tag start.
			changed = true
			b.WriteString(text[start:closePos]) // includes "<![CDATA[" + content
			b.WriteString("]]>")
			pos = closePos + 2 // skip "]]"
		default:
			b.WriteString(text[start : closePos+len(closeMarker)])
			pos = closePos + len(closeMarker)
		}
	}

	if !changed {
		return text
	}
	return b.String()
}

// findLooseCDATAClose returns the relative offset of "]]<TAG" inside text[from:],
// where "<TAG" is heuristically a real tag start (letter, '/', '|', or '｜'
// follows). Used to recover from the common model bug of emitting "]]<" when
// the canonical close is "]]>".
func findLooseCDATAClose(text string, from int) int {
	if from >= len(text) {
		return -1
	}
	for i := from; i+2 < len(text); i++ {
		if text[i] != ']' || text[i+1] != ']' || text[i+2] != '<' {
			continue
		}
		if isLikelyTagStartAt(text, i+2) {
			return i - from
		}
	}
	return -1
}

func isLikelyTagStartAt(text string, idx int) bool {
	if idx >= len(text) || text[idx] != '<' {
		return false
	}
	rest := text[idx+1:]
	if rest == "" {
		return false
	}
	if rest[0] == '/' {
		rest = rest[1:]
	}
	if rest == "" {
		return false
	}
	c := rest[0]
	if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' {
		return true
	}
	if c == '|' {
		return true
	}
	if strings.HasPrefix(rest, "｜") {
		return true
	}
	return false
}
