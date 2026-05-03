package util

import "testing"

func TestTruncateRunesKeepsUnicodeWhole(t *testing.T) {
	got, truncated := TruncateRunes("你好世界", 2)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if got != "你好" {
		t.Fatalf("TruncateRunes() = %q, want %q", got, "你好")
	}
}

func TestTruncateUTF8BytesKeepsRuneBoundary(t *testing.T) {
	got, truncated := TruncateUTF8Bytes("你好a", 4)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if got != "你" {
		t.Fatalf("TruncateUTF8Bytes() = %q, want %q", got, "你")
	}
}
