//go:build ignore

package taxonomy

// fetchCategory builds the one *Response the handlers actually emit. The dead
// extraction flow builds nothing.
func fetchCategory(w http.ResponseWriter, r *http.Request) {
	resp := apiv1.GetCategoryResponse{Name: "racking"}
	shared.WriteJSON(w, resp)
}
