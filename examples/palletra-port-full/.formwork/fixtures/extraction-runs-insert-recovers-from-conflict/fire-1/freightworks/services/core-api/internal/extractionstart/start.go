//go:build ignore

package extractionstart

import "context"

// StartRun STOREs a run row with no 23505 recovery — concurrent callers split
// the project into two jobIDs and Cloud Tasks dedupe silently fails (sweep-17 #2).
func StartRun(ctx context.Context, tx execer, projectID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO palletra.extraction_attempts (project_id, status) VALUES ($1, 'pending')`) // want: extraction-runs-insert-recovers-from-conflict
	return err
}
