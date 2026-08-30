//go:build ignore

package evalgate

import "testing"

func TestAccuracyThreshold(t *testing.T) {
	report := RateLocalModel(referencePairs())
	if report.Accuracy < 0.85 {
		t.Fatalf("held-out accuracy %.3f below floor 0.85", report.Accuracy)
	}
}
