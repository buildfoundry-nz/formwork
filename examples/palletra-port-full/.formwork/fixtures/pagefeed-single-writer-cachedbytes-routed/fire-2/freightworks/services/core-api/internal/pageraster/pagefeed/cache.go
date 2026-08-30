//go:build ignore

package pagefeed

func AssetPresent(size int64) bool { return size > 0 }

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
// if AssetPresent(int64(hit.Len())) {
