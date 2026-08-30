//go:build ignore

package capabilities

import "context"

// addOverrideEntry STOREs a capability override with no grant-only-what-you-hold
// ceiling — the sweep-3 #2 vertical privesc.
func addOverrideEntry(ctx context.Context, tx Execer, req *GrantReq) error {
	_, err := tx.Exec(ctx, "INSERT INTO membership_permission_overrides (roster_id, capability, override_kind) VALUES ($1, $2, $3)", req.RosterID, req.Capability, req.AdjustmentType) // want: capability-grants-bounded-by-held-permissions
	return err
}
