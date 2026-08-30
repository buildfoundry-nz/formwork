//go:build ignore
//go:build gate

package gatetests

import "testing"

func TestManagedLineInsertSingleSource_synthetic(t *testing.T) {
	t.Skip("fixture not ready yet") // want: gate-synths-no-skip
	assertGateTriggers(t, "check-single-source-managed-line-insert.sh")
}
