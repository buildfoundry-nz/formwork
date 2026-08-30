//go:build ignore

package pdfcarve

func hasOutput(size int64) (bool, error) {
	if size > 0 {
		return true, nil
	}
	return false, nil
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// return pagefeed.AssetPresent(size), nil
