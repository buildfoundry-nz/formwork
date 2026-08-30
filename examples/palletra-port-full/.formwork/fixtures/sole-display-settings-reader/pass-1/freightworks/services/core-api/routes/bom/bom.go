//go:build ignore

package bom

func loadSnapshot(ctx context.Context, tx pgx.Tx, projectID string) {
	snap, _ := workflowdata.ReadProjectViewSettingsSnapshot(ctx, tx, projectID)
	_ = snap
}
