//go:build ignore

package lineitems

// createBomLine binds the caller-supplied rated_item id straight into the row
// with no tenant-scoped ownership check. The FK to platform.priced_lines runs as
// table owner and bypasses RLS, so a foreign org's id persists (IDOR).
func createBomLine(ctx Context, tx Tx, req CreateReq) error {
	return tx.Exec(ctx, insertPricingLine, req.BaseRatedItemId) // want: priced-item-bind-checks-tenant-ownership
}
