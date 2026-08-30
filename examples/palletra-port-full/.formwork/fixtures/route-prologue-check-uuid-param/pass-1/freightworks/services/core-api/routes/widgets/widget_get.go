//go:build ignore

package widgets

import "net/http"

// The collapsed form.
func handleGetWidget(w http.ResponseWriter, r *http.Request) {
	id, ok := shared.UUIDArgOr400(w, r, "id")
	if !ok {
		return
	}
	respond(w, loadWidget(r, id))
}

// DecodeUUIDParam stays where the 400 carries a MORE SPECIFIC code than the
// generic bad_request the helper emits — collapsing this one would lose the
// code the client switches on.
func handleGetWidgetRevision(w http.ResponseWriter, r *http.Request) {
	rev, err := shared.DecodeUUIDParam(chi.URLParam(r, "revisionId"))
	if err != nil {
		shared.WriteErrorPayload(w, http.StatusBadRequest, "revision_id_invalid")
		return
	}
	respond(w, loadRevision(r, rev))
}
