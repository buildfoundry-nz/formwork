//go:build ignore

package classify

// composeHint documents the canonical anchor: it joins with " — " between the
// category and name — but this comment-only mention must NOT fire (the em-dash
// lives here only as prose, and ComposeSubcategoryAnchorText is the sole emitter).
func composeHint() string {
	return primaryAnchor()
}
