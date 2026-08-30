//go:build ignore

package widgets

import "net/http"

// The parse-URL-UUID-or-400 preamble, written out by hand.
func handleGetWidget(w http.ResponseWriter, r *http.Request) {
	id, err := shared.DecodeUUIDParam(chi.URLParam(r, "id")) // want: route-prologue-check-uuid-param
	if err != nil {
		shared.WriteErrorPayload(w, http.StatusBadRequest, "bad_request")
		return
	}
	respond(w, loadWidget(r, id))
}
