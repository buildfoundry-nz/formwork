//go:build ignore

package worker

import "net/http"

// handleTask is the generic body every job handler trampolines into. The OIDC
// guard runs FIRST; only an authenticated caller's payload is ever decoded.
func handleTask(w *Worker, rw http.ResponseWriter, r *http.Request) {
	if !w.authenticate(rw, r) {
		http.Error(rw, "unauthorized", http.StatusUnauthorized)
		return
	}
	job, err := parseJobProto(r)
	_ = job
	_ = err
}
