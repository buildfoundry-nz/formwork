//go:build ignore

package shared

// FetchProjectAnnotations reads the whole project's annotations with no LIMIT —
// a runaway project returns an unbounded payload the measure canvas then renders.
func FetchProjectAnnotations(ctx context.Context, tx pgx.Tx, projectID string) error {
	rows, err := tx.Query(ctx, `SELECT id FROM palletra.annotations WHERE project_id = $1`, projectID) // want: project-annotations-read-has-limit
	_ = rows
	return err
}
