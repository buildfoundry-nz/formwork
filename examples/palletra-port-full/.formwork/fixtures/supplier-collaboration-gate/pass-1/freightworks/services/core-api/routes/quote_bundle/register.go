//go:build ignore

package quote_bundle

func Register(rg *Registry, h *QuotePackageHandler) {
	rg.Capability(orgctx.CapSupplierCollabAccess, "/api/quote-bundles/", h.FetchForCollaborator)
}
