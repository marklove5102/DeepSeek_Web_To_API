package toolcall

import (
	"strings"
	"testing"
)

// Each case feeds a string through normalizeDSMLToolCallMarkup (the public
// entry that the rest of the pipeline calls) and asserts the result. Using
// the entry point ensures the regex fallback composes with the existing
// scanner, not just the regex in isolation.

func TestRegexFallbackRewritesDottedDSMLPair(t *testing.T) {
	in := `before <DSML.tool_calls><DSML.invoke name="Read"><DSML.parameter name="path"><![CDATA[/etc/hosts]]></DSML.parameter></DSML.invoke></DSML.tool_calls> after`
	out, _ := normalizeDSMLToolCallMarkup(in)
	mustContain(t, out, "<tool_calls>")
	mustContain(t, out, "</tool_calls>")
	mustContain(t, out, "<invoke")
	mustContain(t, out, "</invoke>")
	mustContain(t, out, "<parameter")
	mustContain(t, out, "</parameter>")
	mustContain(t, out, "/etc/hosts")
}

func TestRegexFallbackRewritesColonSeparatedDSMLPair(t *testing.T) {
	in := `<|DSML : tool_calls|><|DSML : invoke name="X"|></|DSML : invoke|></|DSML : tool_calls|>`
	out, _ := normalizeDSMLToolCallMarkup(in)
	mustContain(t, out, "<tool_calls>")
	mustContain(t, out, "</tool_calls>")
	mustContain(t, out, "<invoke")
	mustContain(t, out, "</invoke>")
}

func TestRegexFallbackRewritesDoubleColonDSMLPair(t *testing.T) {
	in := `<dsml::tool_calls><dsml::invoke name="A"></dsml::invoke></dsml::tool_calls>`
	out, _ := normalizeDSMLToolCallMarkup(in)
	mustContain(t, out, "<tool_calls>")
	mustContain(t, out, "</tool_calls>")
}

// Orphan opener (no matching closer) must be left verbatim — the regex
// pass MUST NOT fabricate a closing tag or rewrite the lone opener into
// canonical XML. This is the safety guarantee around #18 follow-up.
func TestRegexFallbackLeavesOrphanOpenerAlone(t *testing.T) {
	in := `noise <DSML.tool_calls> middle text without close`
	out, _ := normalizeDSMLToolCallMarkup(in)
	if strings.Contains(out, "<tool_calls>") {
		t.Fatalf("orphan opener was rewritten to canonical form: %q", out)
	}
	if out != in {
		t.Fatalf("orphan opener content was mutated; got %q want %q", out, in)
	}
}

// Orphan closer alone is similarly left verbatim.
func TestRegexFallbackLeavesOrphanCloserAlone(t *testing.T) {
	in := `noise without opener </DSML.tool_calls> tail`
	out, _ := normalizeDSMLToolCallMarkup(in)
	if strings.Contains(out, "</tool_calls>") {
		t.Fatalf("orphan closer was rewritten to canonical form: %q", out)
	}
}

// Plain canonical XML must be a no-op (not double-rewritten / corrupted).
func TestRegexFallbackPreservesCanonicalXML(t *testing.T) {
	in := `<tool_calls><invoke name="Read"><parameter name="path">/etc/hosts</parameter></invoke></tool_calls>`
	out, _ := normalizeDSMLToolCallMarkup(in)
	if out != in {
		t.Fatalf("canonical XML was mutated: got %q want %q", out, in)
	}
}

// Already-recognized DSML variant (handled by the existing scanner pass)
// must reach canonical form exactly once — the regex pass running second
// must NOT re-rewrite already-canonical output.
func TestRegexFallbackIdempotentWithExistingScanner(t *testing.T) {
	in := `<|DSML|tool_calls><|DSML|invoke name="X"></|DSML|invoke></|DSML|tool_calls>`
	first, _ := normalizeDSMLToolCallMarkup(in)
	second, _ := normalizeDSMLToolCallMarkup(first)
	if first != second {
		t.Fatalf("normalize is not idempotent\nfirst:  %q\nsecond: %q", first, second)
	}
	mustContain(t, first, "<tool_calls>")
	mustContain(t, first, "</tool_calls>")
}

// Nested same-name pairs (rare but should not crash). Inner pair is paired,
// outer pair is paired — both rewritten.
func TestRegexFallbackHandlesNestedPairs(t *testing.T) {
	in := `<DSML.tool_calls><DSML.tool_calls></DSML.tool_calls></DSML.tool_calls>`
	out, _ := normalizeDSMLToolCallMarkup(in)
	if strings.Count(out, "<tool_calls>") != 2 || strings.Count(out, "</tool_calls>") != 2 {
		t.Fatalf("expected 2 open + 2 close canonical tags; got %q", out)
	}
}

// Content inside CDATA must NOT be regex-matched even if it looks like a
// DSML tag — that's user-supplied parameter content.
func TestRegexFallbackSkipsCDATA(t *testing.T) {
	in := `<tool_calls><invoke name="Echo"><parameter name="text"><![CDATA[<DSML.tool_calls></DSML.tool_calls>]]></parameter></invoke></tool_calls>`
	out, _ := normalizeDSMLToolCallMarkup(in)
	if !strings.Contains(out, "<![CDATA[<DSML.tool_calls></DSML.tool_calls>]]>") {
		t.Fatalf("CDATA content was rewritten by regex pass: %q", out)
	}
}

// Empty / no-DSML-signal inputs should fast-path with zero allocations and
// no behaviour change.
func TestRegexFallbackEarlyOutOnNoDSMLSignal(t *testing.T) {
	in := "plain prose with <html>tags</html> and no pipe or dsml literal"
	out, _ := normalizeDSMLToolCallMarkup(in)
	if out != in {
		t.Fatalf("text without DSML signal was mutated: got %q want %q", out, in)
	}
}

// Lookalike tags like `tool_calls_extra` (canonical-name prefix + suffix)
// must NOT match the regex fallback. The boundary requirement after the
// captured name is the safeguard — without it the regex would absorb
// `_extra` as an attribute and rewrite the tag, which would silently
// reinterpret unrelated content as a tool_calls block. Mirrors the
// existing scanner's hasToolMarkupBoundary contract.
func TestRegexFallbackRejectsLookalikeNamesWithSuffix(t *testing.T) {
	cases := []string{
		`<|DSML tool_calls_extra><|DSML invoke name="X"></|DSML invoke></|DSML tool_calls_extra>`,
		`<DSML.tool_calls_extra><DSML.invoke name="X"></DSML.invoke></DSML.tool_calls_extra>`,
		`<dsml::tool_calls_other></dsml::tool_calls_other>`,
	}
	for _, in := range cases {
		out, _ := normalizeDSMLToolCallMarkup(in)
		if strings.Contains(out, "<tool_calls>") || strings.Contains(out, "</tool_calls>") {
			t.Fatalf("lookalike-name input was rewritten to canonical tool_calls; in=%q out=%q", in, out)
		}
	}
}

func TestRegexFallbackHandlesMixedCanonicalAndRegexVariants(t *testing.T) {
	// Outer is recognized DSML (scanner handles), inner is regex-only variant.
	in := `<|DSML|tool_calls><DSML.invoke name="X"></DSML.invoke></|DSML|tool_calls>`
	out, _ := normalizeDSMLToolCallMarkup(in)
	mustContain(t, out, "<tool_calls>")
	mustContain(t, out, "</tool_calls>")
	mustContain(t, out, "<invoke")
	mustContain(t, out, "</invoke>")
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected to contain %q\ngot %q", needle, haystack)
	}
}
