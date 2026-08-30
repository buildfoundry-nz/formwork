//go:build ignore

package lineitems

// createBomLine verifies the rated_item is owned by the caller's org (RLS
// scoped) before binding it into the row, rejecting a foreign or malformed id.
func createBomLine(ctx Context, tx Tx, req CreateReq) error {
	if err := bomdoc.AssertPricedItemOwned(ctx, tx, req.BaseRatedItemId); err != nil {
		return err
	}
	return tx.Exec(ctx, insertPricingLine, req.BaseRatedItemId)
}
