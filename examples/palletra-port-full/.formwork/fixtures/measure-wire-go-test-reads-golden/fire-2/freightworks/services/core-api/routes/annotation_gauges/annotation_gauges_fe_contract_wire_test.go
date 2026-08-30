//go:build ignore

package annotation_gauges

import "testing"

// Hand-builds the proto in-memory instead of reading the golden from disk.
func uiWireAffectedPage(t *testing.T) *ImpactedPage {
	return &ImpactedPage{SheetId: "p1"}
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// b, err := os.ReadFile("testdata/plot_wire_affected_page.golden.json")
