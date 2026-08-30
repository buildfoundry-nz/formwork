//go:build ignore

package ratecardmatch

func unrelatedFn(s string) string { return s }

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// func CanonCode(s string) string { return s }
