//go:build ignore

package pagecrossbeam

func profile(span float64) string {
	if span <= 1.5 { // want: crossbeam-rule-single-source
		return profileLight
	}
	return profileHeavy
}
