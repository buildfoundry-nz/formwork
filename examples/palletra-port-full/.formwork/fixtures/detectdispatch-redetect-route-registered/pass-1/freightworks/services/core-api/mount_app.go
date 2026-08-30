//go:build ignore

package coreapi

// The per-page recovery route is registered: it releases the page's
// derived-detector latches in-tx (detectdispatch.ClearAllForPages).
func MountApp(r chi.Router, h Handlers) {
	r.Post("/projects/{projectID}/pages/{sheetID}/approve", h.Approve)
	r.Post("/projects/{projectID}/pages/{sheetID}/redetect", h.Redetect)
	r.Get("/projects/{projectID}/pages/{sheetID}", h.GetPage)
}
