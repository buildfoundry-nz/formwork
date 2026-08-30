//go:build ignore

package pricing

import "net/http"

// A revision conflict is an apiv1.ErrorCode member, so the wire string belongs
// to the enum. This prose names "revision_conflict" on purpose: decomment-go
// blanks it, so the ban is asserted against code and not against writing about
// the code.
func patchQuote(w http.ResponseWriter, r *http.Request, err error) {
	if revisionIsBehind(err) {
		shared.WriteErrorPayload(w, http.StatusConflict, "revision_conflict") // want: error-wire-codes-no-raw-literal
		return
	}
	shared.WriteErrorPayload(w, http.StatusForbidden, "capability_disabled") // want: error-wire-codes-no-raw-literal
}
