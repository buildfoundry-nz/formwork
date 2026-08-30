//go:build ignore

package stockroom

// updateRatedItem validates the rate string before persisting, so an invalid
// rate is rejected before it can reach the numeric(12,4) column.
func updateRatedItem(ctx context.Context, tx pgx.Tx, req *UpdateRatedItemRequest) error {
	if err := shared.ValidateRateInput(req.TariffText); err != nil {
		return err
	}
	rate := req.TariffText
	_, err := tx.Exec(ctx, `UPDATE palletra.priced_lines SET rate = COALESCE($1::numeric, rate)`, rate)
	return err
}
