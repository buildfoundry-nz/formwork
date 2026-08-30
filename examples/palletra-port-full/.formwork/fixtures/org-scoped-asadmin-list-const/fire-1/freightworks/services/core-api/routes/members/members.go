//go:build ignore

package members

import "context"

// Decoy: the body carries `registered_by_org = $1`, NOT `org_id = $1`, so the
// BYPASSRLS list is not confined to the caller's org even though the first bind
// is claims.GetTenantId(). fact (a) is violated → this rule fires.
const listClaimsQuery = `SELECT id FROM members WHERE registered_by_org = $1 ORDER BY id LIMIT $2` // want: org-scoped-asadmin-list-const

func (h *Handler) List(ctx context.Context, claims Claims, tx DB, limit int) error {
	rows, err := tx.Query(ctx, listClaimsQuery, claims.GetTenantId(), limit)
	_ = rows
	return err
}
