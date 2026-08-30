//go:build ignore

package bom

import "context"

// Duplicate draft-BOM INSERT outside internal/bomdraft.
func guaranteeDraft(ctx context.Context, tx DbTx, projectID string) error {
	_, err := tx.Exec(ctx, "INSERT INTO palletra.boms (project_id, status) VALUES ($1, 'draft')", projectID) // want: bom-draft-single-source-insert
	return err
}
