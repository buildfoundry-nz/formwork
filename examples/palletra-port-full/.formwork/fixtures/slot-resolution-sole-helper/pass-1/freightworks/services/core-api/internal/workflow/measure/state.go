//go:build ignore

package measure

// deriveState flows through the single helper — no re-inlined co-occurrence of
// ListSlotPages + slots.Resolve.
func deriveState(ctx context.Context, tx pgx.Tx, projectID string, jobType string) ([]Slot, error) {
	return workflowdata.LoadForProjectWithPages(ctx, tx, projectID, jobType)
}
