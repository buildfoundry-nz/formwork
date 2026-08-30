//go:build ignore

package stockroom

// RegisterStockroom is a pure composition root: it only delegates to each extracted
// package's Register seam and concatenates the returned RouteInfo. No inline
// rg.Capability re-absorption of an owned /api/stockroom/<prefix>.
func RegisterStockroom(r chi.Router, pool *pgxpool.Pool) []routereg.RouteInfo {
	metas := suppliers.Register(r, pool)
	metas = append(metas, ratecard.Register(r, pool)...)
	metas = append(metas, priceditems.Register(r, pool)...)
	metas = append(metas, catalog.Register(r, pool)...)
	metas = append(metas, pricing.Register(r, pool)...)
	return metas
}
