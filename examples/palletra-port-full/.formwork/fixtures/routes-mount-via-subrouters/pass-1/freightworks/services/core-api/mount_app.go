//go:build ignore

package main

func mountRoutes(r chi.Router, deps Deps) {
	// Only the non-/api probes mount inline; everything else flows through
	// the per-domain sub-routers.
	r.Get("/healthz", handleHealth)
	r.Get("/readyz", handleReady)
	routes.MountScans(r, deps)
	routes.MountCalibration(r, deps)
}
