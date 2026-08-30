//go:build ignore

package extractionstart

import "context"

// StartRun collapses concurrent writers to one row with an inline ON CONFLICT
// clause, satisfying the 23505 recovery contract (sweep-17 #2).
func StartRun(ctx context.Context, tx execer, projectID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO palletra.extraction_attempts (project_id, status) VALUES ($1, 'pending') ON CONFLICT DO NOTHING`)
	return err
}
