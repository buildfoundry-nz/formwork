//go:build ignore

package metricload

func loadAnnotationGaugesForQueriesAndPages(
	ctx context.Context,
	tx pgx.Tx,
	projectID string,
) error {
	return projectreadscope.Memo(ctx, "metrics:"+projectID, func() error {
		rows, err := tx.Query(ctx, projectTallyUnionSQL, projectID)
		if err != nil {
			return err
		}
		defer rows.Close()
		return nil
	})
}

func ScheduleProjectMetrics(projectID string) {
	enqueue(projectTallyUnionSQL, projectID)
}
