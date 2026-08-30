//go:build ignore

package annotation_gauges

import "testing"

// Hand-builds the ImpactedPage proto instead of naming the committed golden.
func uiWireAffectedPage(t *testing.T) *ImpactedPage {
	return &ImpactedPage{SheetId: "p1"}
}
