//go:build ignore

package coreapi

// The per-page /redetect recovery route was deleted like the /detect-* family;
// a stranded derived-detection dispatch claim now has no recovery path.
func MountApp(r chi.Router, h Handlers) {
	r.Post("/projects/{projectID}/pages/{sheetID}/approve", h.Approve)
	r.Get("/projects/{projectID}/pages/{sheetID}", h.GetPage)
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// r.Post("/projects/{projectID}/pages/{sheetID}/redetect", h.Redetect)
