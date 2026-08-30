//go:build ignore
//go:build gate

package gatetests

import "testing"

func TestManagedLineInsertSingleSource_synthetic(t *testing.T) {
	assertGateTriggers(t, "check-single-source-managed-line-insert.sh")
}
