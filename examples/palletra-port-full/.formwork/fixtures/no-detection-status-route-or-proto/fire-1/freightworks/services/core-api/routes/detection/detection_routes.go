//go:build ignore

package detection

func register(r httpRouter, h *handler) {
	r.Get("/api/projects/{projectID}/detection-status", h.ScanStatus) // want: no-detection-status-route-or-proto
}
