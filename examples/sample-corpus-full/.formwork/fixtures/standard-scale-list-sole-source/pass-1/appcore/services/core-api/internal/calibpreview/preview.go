//go:build ignore

package calibpreview

func recommend(v float64) float64 {
	return scalecalc.ClosestStandardScale(v)
}
