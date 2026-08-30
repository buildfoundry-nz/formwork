//go:build ignore

package annotation_gauges

import "testing"

// Hand-builds the proto in-memory instead of reading the golden from disk.
func uiWireAffectedPage(t *testing.T) *ImpactedPage {
	return &ImpactedPage{SheetId: "p1"}
}
