//go:build ignore

package pricing

import "net/http"

// Same two conditions as the fire tree, emitted through the enum. The codes
// "revision_conflict" and "capability_disabled" appear here only in prose,
// which decomment-go blanks before the pattern runs.
func patchQuote(w http.ResponseWriter, r *http.Request, err error) {
	if revisionIsBehind(err) {
		shared.WriteError(w, apierr.RevisionConflict)
		return
	}
	shared.WriteError(w, apierr.CapabilityDisabled)
}
