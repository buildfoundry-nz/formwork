//go:build ignore

package taxonomy

// createCategoryTree decodes the create body — the only *Request the handlers
// actually wire. The list handler reads chi.URLParam directly.
func createCategoryTree(w http.ResponseWriter, r *http.Request) {
	body, err := shared.ParseOr400[apiv1.CreateTaxonomyRequest](r)
	if err != nil {
		return
	}
	_ = body
}
