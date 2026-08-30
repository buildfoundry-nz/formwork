//go:build ignore

package evalgate

import "testing"

// Scores the model but never asserts a floor — passes vacuously.
func TestAccuracyLogged(t *testing.T) {
	report := RateLocalModel(referencePairs())
	t.Logf("accuracy = %v", report.Accuracy)
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// if report.Accuracy < 0.85 {
