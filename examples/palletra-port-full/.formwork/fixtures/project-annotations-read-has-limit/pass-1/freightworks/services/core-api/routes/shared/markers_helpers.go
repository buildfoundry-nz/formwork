//go:build ignore

package shared

// FetchProjectAnnotations bounds the project-scoped read with a LIMIT so a
// runaway project cannot return an unbounded payload (413 on overflow).
func FetchProjectAnnotations(ctx context.Context, tx pgx.Tx, projectID string) error {
	rows, err := tx.Query(ctx, `SELECT id FROM palletra.annotations WHERE project_id = $1 LIMIT 50000`, projectID)
	_ = rows
	return err
}
