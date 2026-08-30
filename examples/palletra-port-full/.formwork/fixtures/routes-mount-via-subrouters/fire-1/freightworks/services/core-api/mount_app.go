//go:build ignore

package main

func mountRoutes(r chi.Router, rg *routereg.Registrar) {
	rg.Capability("scan.read")             // want: routes-mount-via-subrouters
	r.Get("/api/v1/scans", handleCaptures) // want: routes-mount-via-subrouters
	r.Get("/healthz", handleHealth)
	r.Get("/readyz", handleReady)
}
