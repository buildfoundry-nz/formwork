//go:build ignore

package pages

import "net/http"

// The hand-written claims-nil preamble: three lines that AssertCapability
// already performs, re-typed per handler.
func handleGetPage(w http.ResponseWriter, r *http.Request) {
	claims := shared.ClaimsFrom(r)
	if claims == nil { // want: route-prologue-check-claims-nil
		shared.WriteErrorPayload(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	respond(w, fetchPageRecord(r))
}
