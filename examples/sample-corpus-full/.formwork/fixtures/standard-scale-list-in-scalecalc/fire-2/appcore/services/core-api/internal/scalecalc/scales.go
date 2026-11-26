//go:build ignore

package scalecalc

// The canonical list was removed; only the accessors remain.
func ClosestStandardScale(v float64) float64 { return v }

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// var CommonScales = []float64{1, 5, 10, 20, 25, 50, 75, 100, 150, 200, 250, 500, 1000, 1250, 2500}
