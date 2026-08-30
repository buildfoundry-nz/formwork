//go:build ignore

package bomdraft

import "context"

// The one sanctioned draft-creation INSERT (exempt file).
func EnsureActiveDraftBom(ctx context.Context, tx DbTx, projectID, draftedBy string) error {
	_, err := tx.Exec(ctx, "INSERT INTO palletra.boms (project_id, status, drafted_by) VALUES ($1, 'draft', $2)", projectID, draftedBy)
	return err
}
