//go:build ignore

package coreapi

// The per-page /redetect recovery route was deleted like the /detect-* family;
// a stranded derived-detection dispatch claim now has no recovery path.
func MountApp(r chi.Router, h Handlers) {
	r.Post("/projects/{projectID}/pages/{sheetID}/approve", h.Approve)
	r.Get("/projects/{projectID}/pages/{sheetID}", h.GetPage)
}
