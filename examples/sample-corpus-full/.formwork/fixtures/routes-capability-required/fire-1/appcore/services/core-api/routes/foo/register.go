//go:build ignore

package foo

func Register(r chi.Router) {
	r.Post("/api/foo/thing", createThing) // want: routes-capability-required
}
