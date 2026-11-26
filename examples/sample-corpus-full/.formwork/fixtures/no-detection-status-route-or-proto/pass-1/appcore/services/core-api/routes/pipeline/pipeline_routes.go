//go:build ignore

package pipeline

func register(r httpRouter, h *handler) {
	r.Get("/api/projects/{projectID}/pipeline-status", h.ConveyorStatus)
}
