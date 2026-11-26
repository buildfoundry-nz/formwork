//go:build ignore

package pagecrossbeam

func profile(span float64) string {
	return crossbeam.Resolve(span).Profile
}
