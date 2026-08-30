//go:build ignore

package annotations

// The same handler as the fire tree, reading the policy through the seam. The
// SQL literal it does hold names palletra.projects but carries neither policy
// column, so the statement predicate never selects it.
const partitionWidthRead = `SELECT id, updated_at FROM palletra.projects WHERE id = $1`

func partitionWidth(ctx Context, tx Tx, projectID string) (int, error) {
	policy, err := projectguard.Read(ctx, tx, projectID, projectguard.LockShared)
	if err != nil {
		return 0, err
	}
	return policy.PartitionWidth(), nil
}
