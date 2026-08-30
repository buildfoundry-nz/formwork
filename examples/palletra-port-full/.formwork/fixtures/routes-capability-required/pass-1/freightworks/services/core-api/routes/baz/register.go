//go:build ignore

package baz

func Register(rg *routereg.Registrar) {
	rg.Capability(capBaz, http.MethodPost, "/api/baz", createBaz)
	rg.Exempt("public health check", http.MethodGet, "/api/health", health)
}
