//go:build ignore

package handler

import (
	"fmt"
	"testing"
)

// TestOrders exists to make the `exclude: ["**/*_test.go"]` in every Go rule
// here load-bearing rather than decorative.
//
// It prints — deliberately. Under no-print-debugging's scope this file would
// fire; the exclude is what keeps it quiet, and print debugging in a test is
// genuinely fine. Delete the exclude from
// .formwork/rules/no-print-debugging.yaml and `formwork check` starts failing
// on this line, which is how you know the exclude is doing work.
//
// It also matters to `formwork lint`: an exclude that matches NO files is
// reported as a dead escape hatch, because a rule carrying an exemption for a
// case that no longer exists is a rule nobody has read in a while.
func TestOrders(t *testing.T) {
	fmt.Println("debugging a test is fine")
	if testing.Short() {
		t.Skip("short mode")
	}
}
