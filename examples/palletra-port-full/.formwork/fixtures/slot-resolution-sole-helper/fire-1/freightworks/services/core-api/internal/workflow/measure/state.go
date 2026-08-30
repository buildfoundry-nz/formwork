//go:build ignore

package measure

// deriveState re-inlines the two-step slot resolution instead of calling
// workflowdata.LoadForProjectWithPages — it co-locates BOTH calls.
func deriveState(ctx context.Context, tx pgx.Tx, projectID string, jobType string) ([]Slot, error) {
	pages, err := workflowdata.ListSlotPages(ctx, tx, projectID) // want: slot-resolution-sole-helper
	if err != nil {
		return nil, err
	}
	return slots.Resolve(jobType, pages), nil
}
