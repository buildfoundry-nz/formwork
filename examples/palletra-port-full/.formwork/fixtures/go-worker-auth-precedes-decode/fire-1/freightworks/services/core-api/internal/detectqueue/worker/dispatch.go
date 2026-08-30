//go:build ignore

package worker

import "net/http"

// handleTask is the generic body every job handler trampolines into. Here the
// payload is decoded BEFORE the OIDC guard runs, so unauthenticated bytes reach
// the proto decoder — the exact ordering this gate forbids.
func handleTask(w *Worker, rw http.ResponseWriter, r *http.Request) {
	job, err := parseJobProto(r) // want: go-worker-auth-precedes-decode
	if !w.authenticate(rw, r) {
		http.Error(rw, "unauthorized", http.StatusUnauthorized)
		return
	}
	_ = job
	_ = err
}
