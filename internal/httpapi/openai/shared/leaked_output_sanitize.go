package shared

import (
	"regexp"
	"strings"

	"DeepSeek_Web_To_API/internal/toolcall"
)

var emptyJSONFencePattern = regexp.MustCompile("(?is)```json\\s*```")
var leakedToolCallArrayPattern = regexp.MustCompile(`(?is)\[\{\s*"function"\s*:\s*\{[\s\S]*?\}\s*,\s*"id"\s*:\s*"call[^"]*"\s*,\s*"type"\s*:\s*"function"\s*}\]`)
var leakedToolResultBlobPattern = regexp.MustCompile(`(?is)<\s*\|\s*tool\s*\|\s*>\s*\{[\s\S]*?"tool_call_id"\s*:\s*"call[^"]*"\s*}`)

var leakedThinkTagPattern = regexp.MustCompile(`(?is)</?\s*think\s*>`)

// leakedBOSMarkerPattern matches DeepSeek BOS markers in BOTH forms:
//   - ASCII underscore: <｜begin_of_sentence｜>
//   - U+2581 variant:   <｜begin▁of▁sentence｜>
var leakedBOSMarkerPattern = regexp.MustCompile(`(?i)<[｜\|]\s*begin[_▁]of[_▁]sentence\s*[｜\|]>`)
var leakedPromptRoleMarkerPattern = regexp.MustCompile(`(?i)<[｜\|]\s*(?:system|user)\s*[｜\|]>`)

// Note: marker close fragments accept both ASCII '/' and full-width '／' (U+FF0F)
// because models sometimes emit the full-width form (observed in the wild as
// "<｜Tool／>"). Likewise the trailing fence accepts the full-width "｜" or
// ASCII "|" before the closing ">".
var leakedToolResultStartMarkerPattern = regexp.MustCompile(`(?i)<[｜\|]\s*tool[_▁]results?\s*[/／]?\s*(?:[｜\|])?>`)
var leakedToolResultEndMarkerPattern = regexp.MustCompile(`(?i)<[｜\|]\s*end[_▁](?:f[_▁])?of[_▁](?:tool[_▁]?results?|sentence)\s*[/／]?\s*(?:[｜\|])?>`)

// leakedMetaMarkerPattern matches the remaining DeepSeek special tokens in BOTH forms:
//   - ASCII underscore: <｜end_of_sentence｜>, <｜end_of_toolresults｜>, <｜end_of_instructions｜>
//   - U+2581 variant:   <｜end▁of▁sentence｜>, <｜end▁of▁toolresults｜>, <｜end▁of▁instructions｜>
//
// The trailing close also tolerates full-width '／' so spelling like
// "<｜Tool／>" is removed even though it isn't a valid XML self-close.
var leakedMetaMarkerPattern = regexp.MustCompile(`(?i)<[｜\|]\s*(?:assistant|tool|tool[_▁]results?|end[_▁](?:f[_▁])?of[_▁]sentence|end[_▁](?:f[_▁])?of[_▁]thinking|end[_▁](?:f[_▁])?of[_▁]tool[_▁]?results?|end[_▁](?:f[_▁])?of[_▁]instructions)\s*[/／]?\s*(?:[｜\|])?>`)

// leakedDSMLMarkupFragmentPattern strips raw DSML / tool-markup fragments that
// the streaming sieve failed to capture (typically because the upstream stream
// truncated mid-block). It matches BOTH well-formed openings like
//
//	<|tool_calls>
//	<|DSML|invoke name="Bash">
//	<|DSML|parameter name="command">
//	</|DSML|parameter>
//	</|DSML|invoke>
//	</|DSML|tool_calls>
//
// AND open fragments that lack a closing '>' but were left dangling by a
// truncated stream (e.g. "<|tool_calls" alone on a line). The pattern is
// deliberately limited to known DSML/tool keywords so it does not erase
// unrelated text that happens to start with "<|".
var leakedDSMLMarkupFragmentPattern = regexp.MustCompile(`(?im)<\s*/?\s*\|\s*(?:DSML\s*\|\s*)?(?:tool_calls?|invoke|parameter|tool_use|tool_result|function_call|tool|DSML)(?:[^\n>]*>|[^\n>]*$)`)

// leakedTrailingPipeTagPattern strips a tail like "<|end_of_tool_result|tool_use_error: ..."
// where the marker name and following payload share the same '|' fence without
// a '>' close. We anchor the match to a known marker name to keep the rule
// safe.
var leakedTrailingPipeTagPattern = regexp.MustCompile(`(?i)<\s*\|\s*end[_▁](?:f[_▁])?of[_▁](?:sentence|tool[_▁]?results?|thinking|instructions)\s*\|[^\n>]+`)
var leakedANGTemplatePattern = regexp.MustCompile("(?i)(?:V\\d+Dynamic\\s*)?(?:`?[A-Za-z0-9_./:-]*format)?`?\\s*\\{\\{ANG\\}\\}")

// leakedAgentXMLBlockPatterns catch agent-style XML blocks that leak through
// when the sieve fails to capture them. These are applied only to complete
// wrapper blocks so standalone "<result>" examples in normal output remain
// untouched.
var leakedAgentXMLBlockPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)<attempt_completion\b[^>]*>(.*?)</attempt_completion>`),
	regexp.MustCompile(`(?is)<ask_followup_question\b[^>]*>(.*?)</ask_followup_question>`),
	regexp.MustCompile(`(?is)<new_task\b[^>]*>(.*?)</new_task>`),
}

var leakedAgentWrapperTagPattern = regexp.MustCompile(`(?is)</?(?:attempt_completion|ask_followup_question|new_task)\b[^>]*>`)
var leakedAgentWrapperPlusResultOpenPattern = regexp.MustCompile(`(?is)<(?:attempt_completion|ask_followup_question|new_task)\b[^>]*>\s*<result>`)
var leakedAgentResultPlusWrapperClosePattern = regexp.MustCompile(`(?is)</result>\s*</(?:attempt_completion|ask_followup_question|new_task)\b[^>]*>`)
var leakedAgentResultTagPattern = regexp.MustCompile(`(?is)</?result>`)

func sanitizeLeakedOutput(text string) string {
	return sanitizeLeakedOutputWithOptions(text, true)
}

func sanitizeLeakedOutputPreservingToolMarkup(text string) string {
	return sanitizeLeakedOutputWithOptions(text, false)
}

func sanitizeLeakedOutputWithOptions(text string, stripToolMarkup bool) string {
	if text == "" {
		return text
	}
	out := emptyJSONFencePattern.ReplaceAllString(text, "")
	out = leakedToolCallArrayPattern.ReplaceAllString(out, "")
	out = leakedToolResultBlobPattern.ReplaceAllString(out, "")
	out = stripLeakedToolResultBlocks(out)
	out = truncateAtLeakedPromptRoleMarker(out)
	out = stripDanglingThinkSuffix(out)
	out = leakedThinkTagPattern.ReplaceAllString(out, "")
	out = leakedBOSMarkerPattern.ReplaceAllString(out, "")
	out = leakedPromptRoleMarkerPattern.ReplaceAllString(out, "")
	out = leakedTrailingPipeTagPattern.ReplaceAllString(out, "")
	out = leakedMetaMarkerPattern.ReplaceAllString(out, "")
	out = leakedANGTemplatePattern.ReplaceAllString(out, "")
	if stripToolMarkup {
		out = stripLeakedToolCallWrapperBlocks(out)
		out = leakedDSMLMarkupFragmentPattern.ReplaceAllString(out, "")
	}
	out = sanitizeLeakedAgentXMLBlocks(out)
	return out
}

// LeakedToolResultStreamFilter suppresses leaked DeepSeek/Claude tool_result
// special-token blocks across streamed chunks. A stateless regexp can remove a
// complete block, but streaming can split "<｜tool_result｜>..." and the closing
// marker across different SSE events; this small state machine prevents the
// middle payload from being emitted as visible assistant text.
type LeakedToolResultStreamFilter struct {
	inToolResult bool
}

func (f *LeakedToolResultStreamFilter) Filter(text string) string {
	if text == "" {
		return text
	}
	var out strings.Builder
	remaining := text
	for remaining != "" {
		if f.inToolResult {
			end := leakedToolResultEndMarkerPattern.FindStringIndex(remaining)
			if end == nil {
				return out.String()
			}
			remaining = remaining[end[1]:]
			f.inToolResult = false
			continue
		}
		start := leakedToolResultStartMarkerPattern.FindStringIndex(remaining)
		if start == nil {
			out.WriteString(remaining)
			break
		}
		out.WriteString(remaining[:start[0]])
		tail := remaining[start[1]:]
		end := leakedToolResultEndMarkerPattern.FindStringIndex(tail)
		if end == nil {
			f.inToolResult = true
			break
		}
		remaining = tail[end[1]:]
	}
	return out.String()
}

func stripLeakedToolResultBlocks(text string) string {
	if text == "" {
		return text
	}
	var filter LeakedToolResultStreamFilter
	return filter.Filter(text)
}

func truncateAtLeakedPromptRoleMarker(text string) string {
	loc := leakedPromptRoleMarkerPattern.FindStringIndex(text)
	if loc == nil || loc[0] == 0 {
		return text
	}
	if strings.TrimSpace(text[:loc[0]]) == "" {
		return text
	}
	return text[:loc[0]]
}

func stripLeakedToolCallWrapperBlocks(text string) string {
	if text == "" {
		return text
	}
	var b strings.Builder
	pos := 0
	for pos < len(text) {
		tag, ok := toolcall.FindToolMarkupTagOutsideIgnored(text, pos)
		if !ok {
			b.WriteString(text[pos:])
			break
		}
		if tag.Start > pos {
			b.WriteString(text[pos:tag.Start])
		}
		if tag.Closing || tag.Name != "tool_calls" {
			b.WriteString(text[tag.Start : tag.End+1])
			pos = tag.End + 1
			continue
		}
		closeTag, ok := toolcall.FindMatchingToolMarkupClose(text, tag)
		if !ok {
			b.WriteString(text[tag.Start : tag.End+1])
			pos = tag.End + 1
			continue
		}
		pos = closeTag.End + 1
	}
	return b.String()
}

func stripDanglingThinkSuffix(text string) string {
	matches := leakedThinkTagPattern.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return text
	}
	depth := 0
	lastOpen := -1
	for _, loc := range matches {
		tag := strings.ToLower(text[loc[0]:loc[1]])
		compact := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(tag), " ", ""), "\t", "")
		if strings.HasPrefix(compact, "</") {
			if depth > 0 {
				depth--
				if depth == 0 {
					lastOpen = -1
				}
			}
			continue
		}
		if depth == 0 {
			lastOpen = loc[0]
		}
		depth++
	}
	if depth == 0 || lastOpen < 0 {
		return text
	}
	prefix := text[:lastOpen]
	if strings.TrimSpace(prefix) == "" {
		return ""
	}
	return prefix
}

func sanitizeLeakedAgentXMLBlocks(text string) string {
	out := text
	for _, pattern := range leakedAgentXMLBlockPatterns {
		out = pattern.ReplaceAllStringFunc(out, func(match string) string {
			submatches := pattern.FindStringSubmatch(match)
			if len(submatches) < 2 {
				return match
			}
			// Preserve the inner text so leaked agent instructions do not erase
			// the actual answer, but strip the wrapper/result markup itself.
			return leakedAgentResultTagPattern.ReplaceAllString(submatches[1], "")
		})
	}
	// Fallback for truncated output streams: strip any dangling wrapper tags
	// that were not part of a complete block replacement. If we detect leaked
	// wrapper tags, strip only adjacent <result> tags to avoid exposing agent
	// markup without altering unrelated user-visible <result> examples.
	if leakedAgentWrapperTagPattern.MatchString(out) {
		out = leakedAgentWrapperPlusResultOpenPattern.ReplaceAllStringFunc(out, func(match string) string {
			return leakedAgentResultTagPattern.ReplaceAllString(match, "")
		})
		out = leakedAgentResultPlusWrapperClosePattern.ReplaceAllStringFunc(out, func(match string) string {
			return leakedAgentResultTagPattern.ReplaceAllString(match, "")
		})
		out = leakedAgentWrapperTagPattern.ReplaceAllString(out, "")
	}
	return out
}
