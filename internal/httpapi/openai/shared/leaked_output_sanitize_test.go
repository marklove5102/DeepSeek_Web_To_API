package shared

import "testing"

func TestLeakedToolResultStreamFilterSuppressesSplitBlocks(t *testing.T) {
	var filter LeakedToolResultStreamFilter

	if got := filter.Filter("前置<｜tool_result｜>account.json"); got != "前置" {
		t.Fatalf("unexpected first filtered chunk: %q", got)
	}
	if got := filter.Filter(".bak deleted<|end▁f▁of▁tool_result｜>后置"); got != "后置" {
		t.Fatalf("unexpected second filtered chunk: %q", got)
	}
	if got := filter.Filter("<｜tool_result/>ym.zip deleted<｜end▁f▁of▁sentence｜>完成"); got != "完成" {
		t.Fatalf("unexpected self-closing start filtered chunk: %q", got)
	}
}
