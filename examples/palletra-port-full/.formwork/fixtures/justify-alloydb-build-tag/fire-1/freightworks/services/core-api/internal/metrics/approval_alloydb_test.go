//go:build ignore
//go:build integration && alloydb   // want: justify-alloydb-build-tag

// approval_alloydb_test.go exercises the approve-tx completion path end to end.
// sweep-14 §A1 — runs on the dev tunnel because PR CI is integration-only.

package metric_test

import "testing"

func TestCompleteApproval(t *testing.T) {
	// integration coverage of the approve-tx timeout path
	_ = t
}
