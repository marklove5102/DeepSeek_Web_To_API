package config

import (
	"strings"
	"testing"
)

func TestBuiltinSafetyContainsCoreCategories(t *testing.T) {
	// Sanity: every category the operator was promised is present in the
	// effective list of a clean config (no operator additions, no
	// disables). If anyone removes the R14 / furry / bestiality blocks
	// without explicit policy approval, this test fires.
	cfg := SafetyConfig{}
	content := cfg.EffectiveBannedContent()
	regex := cfg.EffectiveBannedRegex()
	jail := cfg.EffectiveJailbreakPatterns()

	mustContain := func(t *testing.T, label string, list []string, needles ...string) {
		t.Helper()
		joined := strings.ToLower(strings.Join(list, "\n"))
		for _, needle := range needles {
			if !strings.Contains(joined, strings.ToLower(needle)) {
				t.Fatalf("%s missing required pattern %q", label, needle)
			}
		}
	}

	mustContain(t, "banned_content R14", content, "未成年色情", "child porn", "csam", "幼女裸照")
	mustContain(t, "banned_content furry", content, "福瑞色情", "yiff", "anthro porn", "兽人色情")
	mustContain(t, "banned_content bestiality", content, "bestiality", "人兽性交")
	mustContain(t, "banned_content R18", content, "肉棒", "blowjob", "强奸")
	mustContain(t, "banned_content crack", content, "破解软件", "IDA Pro crack")
	mustContain(t, "banned_content CTF", content, "give me the flag", "CTF 答案")
	mustContain(t, "banned_regex R14", regex, "child|minor", "幼|未成年|萝莉")
	mustContain(t, "banned_regex furry", regex, "furry|anthro", "福瑞|兽人")
	mustContain(t, "banned_regex LGBT politicized", regex, "lgbt", "颜色革命", "境外势力")
	mustContain(t, "jailbreak persona", jail, "ignore previous instructions", "you are DAN", "越狱模式")
	mustContain(t, "jailbreak prompt-leak", jail, "leak your system prompt", "泄漏你的系统提示")
}

func TestEffectiveBannedContentMergesUserOnTop(t *testing.T) {
	cfg := SafetyConfig{
		BannedContent: []string{"custom-operator-string-A", "custom-operator-string-B"},
	}
	got := cfg.EffectiveBannedContent()
	if len(got) <= len(cfg.BannedContent) {
		t.Fatalf("expected builtin + user merge to exceed user-only count, got %d (user=%d)", len(got), len(cfg.BannedContent))
	}
	last := got[len(got)-2:]
	if last[0] != "custom-operator-string-A" || last[1] != "custom-operator-string-B" {
		t.Fatalf("expected operator strings to be appended last in order, got %#v", last)
	}
}

func TestDisabledBuiltinRuleIsRespected(t *testing.T) {
	// Pick an AllowDisable=true rule and an AllowDisable=false rule.
	// Confirm only the disable-able one drops out.
	cfg := SafetyConfig{
		DisabledBuiltinRules: []string{
			"r18.cn.act.1",     // AllowDisable=true → must drop
			"r14.csam.cn.1",    // AllowDisable=false → must STAY
			"furry.cn.1",       // AllowDisable=false → must STAY
			"bestiality.en.1",  // AllowDisable=false → must STAY
			"jb.persona.dan.1", // AllowDisable=false → must STAY
		},
	}
	content := cfg.EffectiveBannedContent()
	jail := cfg.EffectiveJailbreakPatterns()
	hasContent := func(needle string) bool {
		for _, p := range content {
			if p == needle {
				return true
			}
		}
		return false
	}
	hasJail := func(needle string) bool {
		for _, p := range jail {
			if p == needle {
				return true
			}
		}
		return false
	}

	if hasContent("肉棒") {
		t.Fatalf("disable of r18.cn.act.1 (AllowDisable=true) was ignored — pattern still present")
	}
	if !hasContent("未成年色情") {
		t.Fatalf("R14 builtin r14.csam.cn.1 was disabled despite AllowDisable=false — policy floor breached")
	}
	if !hasContent("福瑞色情") {
		t.Fatalf("furry builtin furry.cn.1 was disabled despite AllowDisable=false — policy floor breached")
	}
	if !hasContent("bestiality") {
		t.Fatalf("bestiality builtin was disabled despite AllowDisable=false — policy floor breached")
	}
	if !hasJail("ignore previous instructions") {
		t.Fatalf("persona-escape jb.persona.dan.1 was disabled despite AllowDisable=false — protocol-level attack surface re-opened")
	}
}

func TestBuiltinSafetyRuleIDsCoversAllThreeCatalogues(t *testing.T) {
	all := BuiltinSafetyRuleIDs()
	expectedMin := len(BuiltinBannedContent) + len(BuiltinBannedRegex) + len(BuiltinJailbreakPatterns)
	if len(all) != expectedMin {
		t.Fatalf("expected BuiltinSafetyRuleIDs to return all %d rules, got %d", expectedMin, len(all))
	}
	// Every rule has a non-empty stable ID.
	seen := make(map[string]struct{}, len(all))
	for _, r := range all {
		if r.ID == "" {
			t.Fatalf("builtin rule with empty ID: %#v", r)
		}
		if _, dup := seen[r.ID]; dup {
			t.Fatalf("duplicate builtin rule ID %q", r.ID)
		}
		seen[r.ID] = struct{}{}
	}
}
