//go:build ignore

package taxonomy

// Both *Request messages are decoded by a real handler, so neither is orphan.
func listClassification(w http.ResponseWriter, r *http.Request) {
	filter, err := shared.ParseOr400[apiv1.ListClassificationRequest](r)
	if err != nil {
		return
	}
	_ = filter
}

func createCategoryTree(w http.ResponseWriter, r *http.Request) {
	body, err := shared.ParseOr400[apiv1.CreateTaxonomyRequest](r)
	if err != nil {
		return
	}
	_ = body
}
