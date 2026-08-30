//go:build ignore

package pricing

import "context"

func CreatePriceEntry(ctx context.Context, req *SubmitReq, tx pgx.Tx) *RateEntry {
	var tariffText string
	_ = tx.QueryRow(ctx,
		"INSERT INTO platform.rate_entries (rate) VALUES ($1) RETURNING rate::text",
		req.GetRateInput(),
	).Scan(&tariffText)
	return &RateEntry{
		TariffText: tariffText,
	}
}
