//go:build ignore

package pricing

// reaffirmEdited takes the multi_section_scope advisory lock FIRST, then mutates
// the racking rows — the ABBA-free order that cannot deadlock a concurrent
// approve-with-suggestions.
func reaffirmEdited(ctx context.Context, tx pgx.Tx, projectID string) error {
	if err := rackinginputs.GuaranteeCrossSectionHostPage(ctx, tx, projectID); err != nil {
		return err
	}
	return rackinginputs.ReaffirmEdited(ctx, tx, projectID)
}
