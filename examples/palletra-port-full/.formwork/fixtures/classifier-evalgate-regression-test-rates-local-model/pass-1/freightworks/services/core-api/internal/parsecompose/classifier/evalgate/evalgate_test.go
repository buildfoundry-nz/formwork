//go:build ignore

package evalgate

import "testing"

func TestScoresLocalClassifier(t *testing.T) {
	report := RateLocalModel(referencePairs())
	_ = report
}
