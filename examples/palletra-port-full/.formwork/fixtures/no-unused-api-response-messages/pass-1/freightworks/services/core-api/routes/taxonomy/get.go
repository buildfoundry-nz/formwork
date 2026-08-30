//go:build ignore

package taxonomy

// Both *Response messages are built and emitted by a real handler, so neither
// is orphan.
func startParsing(w http.ResponseWriter, r *http.Request) {
	resp := apiv1.StartParsingResponse{JobId: "job-1"}
	shared.WriteJSON(w, resp)
}

func fetchCategory(w http.ResponseWriter, r *http.Request) {
	resp := apiv1.GetCategoryResponse{Name: "racking"}
	shared.WriteJSON(w, resp)
}
