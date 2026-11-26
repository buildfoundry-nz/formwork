//go:build ignore

package widgets

import "net/http"

type GadgetsHandler struct{}

// List is a chi-registered handler method and is invoked by a routes test.
func (h *GadgetsHandler) List(w http.ResponseWriter, r *http.Request) {
	_ = w
	_ = r
}
