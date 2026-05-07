package toolcall

import (
	"regexp"
	"sort"
	"strings"
)

// Regex-based DSML tag rescue.
//
// The character-by-character scanner in toolcalls_scan.go handles the DSML
// variants we have explicitly catalogued (ASCII / fullwidth pipes, space /
// hyphen / underscore separators, zero-width artifacts, Chinese names).
// Models keep emitting new wrapper shapes — `<DSML.tool_calls>`,
// `<|DSML : tool_calls|>`, `<dsml::tool_calls>`, `<【DSML】tool_calls>` etc.
// Each of those used to require a focused scanner patch + regression test.
//
// This file adds a tolerant regex pass that runs AFTER the scanner, so it
// only sees variants the scanner missed. It rewrites a tag only when the
// opener AND its matching closer are both found — paired-tag requirement
// from #18 follow-up. Lone openers / closers are left verbatim so we cannot
// turn arbitrary user-visible content into a fake tool_calls tag.

// dsmlNamespaceSepClass matches separator / decoration characters that can
// appear inside a DSML tag between '<' (or '</'), an optional DSML prefix,
// and the canonical tool-call name. Excludes '>' (tag terminator) and '/'
// (closer/self-close marker) so the regex cannot run past the tag bounds.
const dsmlNamespaceSepClass = `[\s\|\x{FF5C}_\-\x{200B}-\x{200D}\x{2060}\x{FEFF}\x{2581}\.\:\;]`

// dsmlNameAlternation captures any recognized canonical or variant tag name.
const dsmlNameAlternation = `(tool[_\-]calls?|invoke|parameter|工具调用|调用|参数)`

// dsmlSignalRequired requires at least one DSML signal — either a pipe
// (ASCII '|' / fullwidth '｜') or the literal "dsml" prefix — between the
// bracket and the name. Without this guard the regex would also match
// canonical XML like `<tool_calls>` which the rest of the parser already
// handles (and which is NOT a DSML variant, so rewriting it is wrong).
const dsmlSignalRequired = `(?:[\|\x{FF5C}]|dsml)`

// dsmlTagTrailingCapture captures the segment between the canonical name
// and the closing '>'. The name MUST be followed by a tag boundary
// character (whitespace / pipe / fullwidth-pipe / '/'), otherwise this is
// a lookalike like `tool_calls_extra` (an unrelated tag that merely
// shares the canonical-name prefix) and we refuse to match — that
// safeguards the parity with the existing char-by-char scanner's
// hasToolMarkupBoundary check.
//
// Attributes (`name="Read"`) following the boundary are preserved verbatim
// so the rewritten canonical tag still carries the parameter metadata
// downstream. We deliberately stop at the first `>` to avoid spilling
// into the next tag; malformed attribute values containing a literal `>`
// will cut the match short, which is the safe failure mode (unmatched →
// orphan → left verbatim).
const dsmlTagTrailingCapture = `(?:[\s\|\x{FF5C}/]([^>]*))?>`

var (
	dsmlOpenRegex = regexp.MustCompile(
		`(?i)<` +
			dsmlNamespaceSepClass + `*` +
			dsmlSignalRequired +
			dsmlNamespaceSepClass + `*` +
			dsmlNameAlternation +
			dsmlTagTrailingCapture,
	)

	dsmlCloseRegex = regexp.MustCompile(
		`(?i)<` +
			dsmlNamespaceSepClass + `*` +
			`/` +
			dsmlNamespaceSepClass + `*` +
			dsmlSignalRequired +
			dsmlNamespaceSepClass + `*` +
			dsmlNameAlternation +
			dsmlTagTrailingCapture,
	)
)

type dsmlRegexMatch struct {
	start     int
	end       int // exclusive
	closing   bool
	canonical string // canonicalized name ("tool_calls" / "invoke" / "parameter")
	attrs     string // raw attribute text between the name and `>` (already trimmed of DSML decorations)
}

// rewriteDSMLToolMarkupOutsideIgnoredRegex is a tolerant SECOND pass for
// DSML variants the char-by-char scanner did not recognize. Only paired
// open+close tags are rewritten — orphans are left in place so we cannot
// silently turn arbitrary content into a tool-calls tag.
func rewriteDSMLToolMarkupOutsideIgnoredRegex(text string) string {
	if text == "" {
		return text
	}
	// Cheap early-out: no DSML signal, no work.
	if !containsDSMLSignal(text) {
		return text
	}

	// Chunk text by ignored XML sections so the regex only runs on
	// content that could legitimately contain tool markup.
	matches := collectDSMLRegexMatchesOutsideIgnored(text)
	if len(matches) == 0 {
		return text
	}

	approved := pairDSMLRegexMatches(matches)
	if len(approved) == 0 {
		return text
	}

	return spliceDSMLRegexRewrites(text, matches, approved)
}

// containsDSMLSignal does the cheap pipe / "dsml" probe so we skip the
// regex compilation hot path on the (vast) majority of payloads that have
// no DSML wrapper at all.
func containsDSMLSignal(text string) bool {
	if strings.ContainsAny(text, "|｜") {
		return true
	}
	// "dsml" check is case-insensitive; bytes.IndexAny would be tighter
	// but ToLower allocation cost is negligible vs the regex pass we'd
	// otherwise pay.
	return strings.Contains(strings.ToLower(text), "dsml")
}

// collectDSMLRegexMatchesOutsideIgnored runs the open/close regexes only on
// segments outside ignored XML sections (CDATA / comments / etc.) and
// returns matches in source-position order. Positions are absolute offsets
// in `text`.
func collectDSMLRegexMatchesOutsideIgnored(text string) []dsmlRegexMatch {
	out := make([]dsmlRegexMatch, 0, 4)
	lower := strings.ToLower(text)

	// segmentStart tracks where the current "outside ignored" segment
	// began. We accumulate matches lazily by running the regex on each
	// such segment.
	segmentStart := 0
	flushSegment := func(end int) {
		if segmentStart >= end {
			return
		}
		seg := text[segmentStart:end]
		appendDSMLRegexMatchesFromSegment(&out, seg, segmentStart)
	}

	for i := 0; i < len(text); {
		next, advanced, blocked := skipXMLIgnoredSection(lower, i)
		if blocked {
			flushSegment(i)
			return out
		}
		if advanced {
			flushSegment(i)
			segmentStart = next
			i = next
			continue
		}
		i++
	}
	flushSegment(len(text))

	sort.SliceStable(out, func(i, j int) bool { return out[i].start < out[j].start })
	return out
}

func appendDSMLRegexMatchesFromSegment(dst *[]dsmlRegexMatch, segment string, base int) {
	// Closers first so we can skip those positions when scanning openers
	// (an "</X>" trivially also matches our opener regex if we let it).
	// Submatch index layout per regex: [0:1] whole match, [2:3] name
	// capture, [4:5] attribute capture (between name and `>`).
	closes := dsmlCloseRegex.FindAllStringSubmatchIndex(segment, -1)
	for _, m := range closes {
		if len(m) < 6 {
			continue
		}
		raw := segment[m[2]:m[3]]
		canonical := canonicalToolMarkupName(strings.ToLower(raw))
		// Closers don't carry meaningful attributes; drop the trailing
		// capture to keep the rewrite output tight (`</NAME>`).
		*dst = append(*dst, dsmlRegexMatch{
			start:     base + m[0],
			end:       base + m[1],
			closing:   true,
			canonical: canonical,
		})
	}

	closeRanges := make([][2]int, 0, len(closes))
	for _, m := range closes {
		closeRanges = append(closeRanges, [2]int{m[0], m[1]})
	}

	opens := dsmlOpenRegex.FindAllStringSubmatchIndex(segment, -1)
	for _, m := range opens {
		if len(m) < 6 {
			continue
		}
		// Skip openers that overlap an already-recognized closer.
		// (Some closers structurally also satisfy the opener regex if read
		// without the leading '/'.)
		if matchOverlapsRanges(m[0], m[1], closeRanges) {
			continue
		}
		raw := segment[m[2]:m[3]]
		canonical := canonicalToolMarkupName(strings.ToLower(raw))
		// The attribute capture group is optional in the regex (the
		// boundary char + attrs collectively absent when the tag is
		// `<...NAME>`). RE2 returns -1 / -1 in that case; guard before
		// slicing.
		var attrs string
		if m[4] >= 0 && m[5] >= 0 && m[5] > m[4] {
			attrs = sanitizeDSMLAttrCapture(segment[m[4]:m[5]])
		}
		*dst = append(*dst, dsmlRegexMatch{
			start:     base + m[0],
			end:       base + m[1],
			closing:   false,
			canonical: canonical,
			attrs:     attrs,
		})
	}
}

// sanitizeDSMLAttrCapture trims the leading DSML decoration (residual
// pipes, fullwidth pipes, and surrounding whitespace) that the regex's
// attribute capture inevitably absorbs, so the rewritten canonical tag
// looks like `<invoke name="X">` rather than `<invoke| name="X">`.
func sanitizeDSMLAttrCapture(raw string) string {
	raw = strings.TrimRight(raw, " \t\r\n|｜/")
	raw = strings.TrimLeft(raw, " \t\r\n|｜")
	if raw == "" {
		return ""
	}
	// Re-prefix a single space so the rewrite emits `<NAME attrs>` rather
	// than `<NAMEattrs>`.
	return " " + raw
}

func matchOverlapsRanges(start, end int, ranges [][2]int) bool {
	for _, r := range ranges {
		if start < r[1] && end > r[0] {
			return true
		}
	}
	return false
}

// pairDSMLRegexMatches walks matches in source order and returns the index
// of every match that participates in a balanced open/close pair. Pairing
// is per canonical name and depth-aware (nested same-name pairs are
// supported, mismatched names are dropped).
func pairDSMLRegexMatches(matches []dsmlRegexMatch) map[int]struct{} {
	approved := make(map[int]struct{})
	stacks := make(map[string][]int) // canonical name → stack of opener indices

	for idx, m := range matches {
		if m.canonical == "" {
			continue
		}
		if !m.closing {
			stacks[m.canonical] = append(stacks[m.canonical], idx)
			continue
		}
		stack := stacks[m.canonical]
		if len(stack) == 0 {
			// Unmatched closer: drop, do not approve.
			continue
		}
		openerIdx := stack[len(stack)-1]
		stacks[m.canonical] = stack[:len(stack)-1]
		approved[openerIdx] = struct{}{}
		approved[idx] = struct{}{}
	}
	return approved
}

// spliceDSMLRegexRewrites replays `text`, replacing each approved match
// with its canonical XML form. Unapproved (orphan) matches and
// non-tag bytes are emitted unchanged.
func spliceDSMLRegexRewrites(text string, matches []dsmlRegexMatch, approved map[int]struct{}) string {
	if len(approved) == 0 {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	cursor := 0
	for idx, m := range matches {
		if _, ok := approved[idx]; !ok {
			continue
		}
		if cursor < m.start {
			b.WriteString(text[cursor:m.start])
		}
		if m.closing {
			b.WriteString("</")
			b.WriteString(m.canonical)
		} else {
			b.WriteByte('<')
			b.WriteString(m.canonical)
			if m.attrs != "" {
				b.WriteString(m.attrs)
			}
		}
		b.WriteByte('>')
		cursor = m.end
	}
	if cursor < len(text) {
		b.WriteString(text[cursor:])
	}
	return b.String()
}
