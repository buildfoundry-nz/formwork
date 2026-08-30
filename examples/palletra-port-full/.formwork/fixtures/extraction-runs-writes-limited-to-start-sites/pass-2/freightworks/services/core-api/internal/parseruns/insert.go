//go:build ignore

package parseruns

import "context"

// insert.go is an allowlisted extraction-START site (excluded from the gate),
// so its INSERT / InsertOrClaimActive must NOT fire.
func InsertOrClaimActive(ctx context.Context, tx Tx, projectID string) {
	tx.Exec(ctx, `INSERT INTO palletra.extraction_attempts (project_id) VALUES ($1)`, projectID)
}
