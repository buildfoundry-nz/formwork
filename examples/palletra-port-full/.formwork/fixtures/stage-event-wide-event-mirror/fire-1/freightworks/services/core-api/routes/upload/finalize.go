//go:build ignore

package upload

func finalize(ctx context.Context, tx pgx.Tx) error {
	const q = "INSERT INTO palletra.workflow_stage_events (id, kind) VALUES ($1, $2)" // want: stage-event-wide-event-mirror
	_, err := tx.Exec(ctx, q, id, kind)
	return err
}
