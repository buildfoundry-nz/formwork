//go:build ignore

package detection

import "context"

func status(ctx context.Context, tx Tx, projectID string) {
	parseruns.InsertOrClaimActive(ctx, tx, projectID)                                            // want: extraction-runs-writes-limited-to-start-sites
	tx.Exec(ctx, `INSERT INTO palletra.extraction_attempts (project_id) VALUES ($1)`, projectID) // want: extraction-runs-writes-limited-to-start-sites
}
