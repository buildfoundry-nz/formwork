//go:build ignore

package stockroom

// updateRatedItem binds the request rate string straight into the UPDATE
// without validating it first — a malformed rate reaches numeric(12,4) and 500s.
func updateRatedItem(ctx context.Context, tx pgx.Tx, req *UpdateRatedItemRequest) error {
	rate := req.TariffText // want: rate-text-must-validate-before-persist
	_, err := tx.Exec(ctx, `UPDATE palletra.priced_lines SET rate = COALESCE($1::numeric, rate)`, rate)
	return err
}
