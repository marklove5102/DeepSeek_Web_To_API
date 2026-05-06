package settings

import "testing"

func TestParseSettingsUpdateRequestSafetyConfig(t *testing.T) {
	enabled := true
	req := map[string]any{
		"safety": map[string]any{
			"enabled":                      enabled,
			"block_message":                "blocked",
			"blocked_ips":                  []any{"203.0.113.10", "198.51.100.0/24"},
			"blocked_conversation_ids":     "conv-1\nconv-2",
			"banned_content":               []any{"secret phrase"},
			"banned_regex":                 []any{"(?i)do not allow"},
			"jailbreak":                    map[string]any{"enabled": true, "patterns": "ignore guardrails"},
			"unused_forward_compatibility": "ignored",
		},
	}

	_, _, _, _, _, _, _, _, _, safety, _, err := parseSettingsUpdateRequest(req)
	if err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	if safety == nil || safety.Enabled == nil || !*safety.Enabled {
		t.Fatalf("expected enabled safety config, got %#v", safety)
	}
	if safety.BlockMessage != "blocked" {
		t.Fatalf("block message=%q", safety.BlockMessage)
	}
	if len(safety.BlockedIPs) != 2 || safety.BlockedIPs[1] != "198.51.100.0/24" {
		t.Fatalf("blocked ips=%v", safety.BlockedIPs)
	}
	if len(safety.BlockedConversationIDs) != 2 || safety.BlockedConversationIDs[1] != "conv-2" {
		t.Fatalf("blocked conversation ids=%v", safety.BlockedConversationIDs)
	}
	if len(safety.BannedRegex) != 1 || safety.BannedRegex[0] != "(?i)do not allow" {
		t.Fatalf("banned regex=%v", safety.BannedRegex)
	}
	if safety.Jailbreak.Enabled == nil || !*safety.Jailbreak.Enabled || len(safety.Jailbreak.Patterns) != 1 {
		t.Fatalf("jailbreak=%#v", safety.Jailbreak)
	}
}

func TestParseSettingsUpdateRequestRejectsInvalidSafetyRegex(t *testing.T) {
	req := map[string]any{
		"safety": map[string]any{
			"enabled":      true,
			"banned_regex": []any{"["},
		},
	}

	_, _, _, _, _, _, _, _, _, _, _, err := parseSettingsUpdateRequest(req)
	if err == nil {
		t.Fatal("expected invalid safety regex error")
	}
}

func TestParseSettingsUpdateRequestResponseCacheConfig(t *testing.T) {
	semanticKey := false
	req := map[string]any{
		"cache": map[string]any{
			"response": map[string]any{
				"dir":                "data/cache2",
				"memory_ttl_seconds": float64(300),
				"memory_max_bytes":   float64(1024),
				"disk_ttl_seconds":   float64(14400),
				"disk_max_bytes":     "4096",
				"max_body_bytes":     float64(2048),
				"semantic_key":       semanticKey,
			},
		},
	}

	_, _, _, _, _, cacheCfg, _, _, _, _, _, err := parseSettingsUpdateRequest(req)
	if err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	if cacheCfg == nil {
		t.Fatal("expected cache config")
	}
	if cacheCfg.Response.Dir != "data/cache2" {
		t.Fatalf("dir=%q", cacheCfg.Response.Dir)
	}
	if cacheCfg.Response.MemoryTTLSeconds != 300 || cacheCfg.Response.DiskTTLSeconds != 14400 {
		t.Fatalf("unexpected ttl config: %#v", cacheCfg.Response)
	}
	if cacheCfg.Response.MemoryMaxBytes != 1024 || cacheCfg.Response.DiskMaxBytes != 4096 || cacheCfg.Response.MaxBodyBytes != 2048 {
		t.Fatalf("unexpected size config: %#v", cacheCfg.Response)
	}
	if cacheCfg.Response.SemanticKey == nil || *cacheCfg.Response.SemanticKey {
		t.Fatalf("semantic_key=%#v", cacheCfg.Response.SemanticKey)
	}
}

func TestParseSettingsUpdateRequestRejectsInvalidResponseCacheLimit(t *testing.T) {
	req := map[string]any{
		"cache": map[string]any{
			"response": map[string]any{
				"memory_max_bytes": float64(0),
			},
		},
	}

	_, _, _, _, _, _, _, _, _, _, _, err := parseSettingsUpdateRequest(req)
	if err == nil {
		t.Fatal("expected invalid cache limit error")
	}
}
