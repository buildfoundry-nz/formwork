//go:build ignore

package members

import "context"

// listClaimsQuery is org-scoped (org_id = $1) — fact (a) is satisfied here so only the
// first-bind rule fires on this fixture.
const listClaimsQuery = `SELECT id, org_id FROM members WHERE org_id = $1 ORDER BY id LIMIT $2`

func (h *Handler) List(ctx context.Context, r *Request, tx DB, limit int) error {
	rows, err := tx.Query(ctx, listClaimsQuery, r.URL.Query().Get("org_id"), limit) // want: asadmin-list-claims-bound-first-param
	_ = rows
	return err
}
