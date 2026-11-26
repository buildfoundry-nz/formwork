//go:build ignore

package quote_bundle

import "net/http"

func (h *QuotePackageHandler) FetchForCollaborator(w http.ResponseWriter, r *http.Request) {
	if err := suppliercollab.RequireActive(r.Context(), h.db, h.tenantID); err != nil {
		http.Error(w, "disabled", http.StatusForbidden)
		return
	}
	pkg, err := h.repo.Load(r.Context())
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, pkg)
}
