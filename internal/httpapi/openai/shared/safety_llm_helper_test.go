package shared

import "testing"

// v1.0.19: PickAuditText is the audit-input selector. Prefer the
// extracted user text; only fall back to FinalPrompt when extraction
// produced nothing (no user message present, or all blocks non-text).
func TestPickAuditTextPrefersLatestUserText(t *testing.T) {
	got := PickAuditText("hello", "system+history+banner+hello")
	if got != "hello" {
		t.Errorf("PickAuditText = %q, want %q", got, "hello")
	}
}

func TestPickAuditTextFallsBackToFinalPromptWhenLatestEmpty(t *testing.T) {
	got := PickAuditText("", "fallback content")
	if got != "fallback content" {
		t.Errorf("PickAuditText fallback = %q, want %q", got, "fallback content")
	}
}

func TestPickAuditTextEmptyBoth(t *testing.T) {
	got := PickAuditText("", "")
	if got != "" {
		t.Errorf("PickAuditText empty = %q, want empty", got)
	}
}
