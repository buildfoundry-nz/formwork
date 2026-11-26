//go:build ignore

package evalgate

import "testing"

// Scores the model but never asserts a floor — passes vacuously.
func TestAccuracyLogged(t *testing.T) {
	report := RateLocalModel(referencePairs())
	t.Logf("accuracy = %v", report.Accuracy)
}
