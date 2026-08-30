//go:build ignore

package annotation_gauges

import (
	"os"
	"testing"
)

func uiWireAffectedPage(t *testing.T) []byte {
	b, err := os.ReadFile("testdata/plot_wire_affected_page.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}
