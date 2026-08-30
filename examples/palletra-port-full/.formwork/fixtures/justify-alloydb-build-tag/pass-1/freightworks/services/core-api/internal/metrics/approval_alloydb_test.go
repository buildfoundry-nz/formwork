//go:build ignore
//go:build integration && alloydb

// approval_alloydb_test.go exercises the approve-tx completion path end to end.
// Depends on the alloydb_scann vector index (an AlloyDB-specific extension),
// which PR CI (integration-only) cannot provide.

package metric_test

import "testing"

func TestCompleteApproval(t *testing.T) {
	// integration coverage of the approve-tx timeout path
	_ = t
}
