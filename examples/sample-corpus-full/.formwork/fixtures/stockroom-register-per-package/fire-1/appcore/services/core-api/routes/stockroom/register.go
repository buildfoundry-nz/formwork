//go:build ignore

package stockroom

// RegisterStockroom should be a pure composition root, but this revision re-inlines
// the suppliers subdomain's routes instead of delegating to suppliers.Register.
func RegisterStockroom(r chi.Router, pool *pgxpool.Pool) []routereg.RouteInfo {
	metas := suppliers.Register(r, pool)
	metas = append(metas, ratecard.Register(r, pool)...)
	rg.Capability(r, "/api/stockroom/suppliers", suppliersIndex(pool)) // want: stockroom-register-per-package
	return metas
}
