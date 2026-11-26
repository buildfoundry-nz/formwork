//go:build ignore

package capabilities

import "context"

// addOverrideEntry rejects a GRANT of any capability the caller does not itself hold
// before writing the override — the grant-only-what-you-hold ceiling.
func addOverrideEntry(ctx context.Context, tx Execer, req *GrantReq, claims Claims) error {
	if req.GetGrantType() == "GRANT" && !callerHasPermission(claims.GetResolvedPermissions(), req.GetEntitlement()) {
		return errForbidden
	}
	_, err := tx.Exec(ctx, "INSERT INTO membership_permission_overrides (roster_id, capability, override_kind) VALUES ($1, $2, $3)", req.RosterID, req.Capability, req.AdjustmentType)
	return err
}
