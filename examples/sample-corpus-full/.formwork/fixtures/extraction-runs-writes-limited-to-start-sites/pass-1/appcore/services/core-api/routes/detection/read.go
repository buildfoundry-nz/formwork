//go:build ignore

package detection

import "context"

func read(ctx context.Context, tx Tx, projectID string) {
	run := locateExtractionRun(ctx, tx, projectID)
	_ = run
}
