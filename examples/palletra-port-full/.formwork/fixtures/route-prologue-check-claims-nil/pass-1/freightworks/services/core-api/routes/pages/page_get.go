//go:build ignore

package pages

import "net/http"

// The collapsed form: one helper that 401s nil claims itself.
func handleGetPage(w http.ResponseWriter, r *http.Request) {
	claims, ok := shared.ClaimsOrUnauthorized(w, r)
	if !ok {
		return
	}
	respond(w, fetchPageRecord(r, claims))
}

// A claims-nil test that is NOT the 401 preamble stays legal: the window that
// makes the preamble a finding is the http.StatusUnauthorized write within
// three lines, and this branch has no wire write at all.
func pageOwner(claims *Claims) string {
	if claims == nil {
		return ""
	}
	return claims.Subject
}
