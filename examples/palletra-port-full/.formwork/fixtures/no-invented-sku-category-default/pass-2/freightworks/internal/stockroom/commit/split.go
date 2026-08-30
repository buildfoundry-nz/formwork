//go:build ignore

package commit

// References platform.sku_catalog (in scope for the gate), but the
// category assignment is split across lines. Line-anchored matching (the
// .sh's per-line grep, restored when the rule left multiline mode)
// deliberately does not see this shape — this fixture pins that narrowing as
// a decision, not an accident. See the rule comment.
func commitSplit() {
	_ = platform.sku_catalog
	category :=
		"general"
	_ = category
}
