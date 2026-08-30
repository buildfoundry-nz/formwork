//go:build ignore

package bar

func Register(r chi.Router) {
	r.With(orgctx.AssertCapability(capBar)).Get("/api/bar", listBaz) // want: routes-capability-required
}
