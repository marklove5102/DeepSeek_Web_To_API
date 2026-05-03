package claude

import "testing"

func TestCleanVisibleOutputUsesSharedLeakSanitizer(t *testing.T) {
	got := cleanVisibleOutput("A<｜begin▁of▁sentence｜>BV3Dynamic `earsformat{{ANG}}C", false)
	if got != "ABC" {
		t.Fatalf("unexpected cleaned output: %q", got)
	}
}
