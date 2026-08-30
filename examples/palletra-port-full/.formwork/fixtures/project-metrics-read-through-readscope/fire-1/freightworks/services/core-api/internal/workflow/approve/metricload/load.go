//go:build ignore

package metricload

func loadAnnotationGaugesForQueriesAndPages(
	ctx context.Context,
	tx pgx.Tx,
	projectID string,
) error {
	rows, err := tx.Query(ctx, projectTallyUnionSQL, projectID) // want: project-metrics-read-through-readscope
	if err != nil {
		return err
	}
	defer rows.Close()
	return nil
}

func loadMemoized(ctx context.Context, tx pgx.Tx, projectID string) error {
	return projectreadscope.Memo(ctx, "metrics:"+projectID, func() error {
		_, err := tx.Query(ctx, projectTallyUnionSQL, projectID)
		return err
	})
}

func ScheduleProjectMetrics(projectID string) {
	// Names the builder, issues no query — must not oblige Memo.
	enqueue(projectTallyUnionSQL, projectID)
}
