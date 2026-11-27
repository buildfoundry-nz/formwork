//go:build ignore

package pagefeed

// The presence rule was inlined here instead of a single AssetPresent def.
func StoredBytes(hit Hit) ([]byte, bool) {
	if hit.Len() > 0 {
		return hit.Bytes(), true
	}
	return nil, false
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// func AssetPresent(size int64) bool { return size > 0 }
