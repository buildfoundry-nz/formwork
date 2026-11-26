//go:build ignore

package pricing

// reaffirmEdited mutates the racking page_gauges rows BEFORE it takes the
// multi_section_scope advisory lock — an ABBA deadlock against a concurrent
// approve-with-suggestions, and a silently lost approve.
func reaffirmEdited(ctx context.Context, tx pgx.Tx, projectID string) error {
	if err := rackinginputs.ReaffirmEdited(ctx, tx, projectID); err != nil { // want: racking-advisory-lock-first
		return err
	}
	return rackinginputs.GuaranteeCrossSectionHostPage(ctx, tx, projectID)
}
