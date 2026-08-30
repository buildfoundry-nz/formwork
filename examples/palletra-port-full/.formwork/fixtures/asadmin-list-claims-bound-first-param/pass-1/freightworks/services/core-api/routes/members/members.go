//go:build ignore

package members

import "context"

const listClaimsQuery = `SELECT id, org_id FROM members WHERE org_id = $1 ORDER BY id LIMIT $2`

func (h *Handler) List(ctx context.Context, claims Claims, tx DB, limit int) error {
	rows, err := tx.Query(ctx, listClaimsQuery, claims.GetTenantId(), limit)
	_ = rows
	return err
}
