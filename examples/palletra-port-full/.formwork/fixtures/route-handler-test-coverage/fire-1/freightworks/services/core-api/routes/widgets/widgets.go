//go:build ignore

package widgets

import "net/http"

type GadgetsHandler struct{}

// List is a chi-registered handler method, but no test in routes/ invokes it —
// audit B18: handler exists, zero test mentions.
func (h *GadgetsHandler) List(w http.ResponseWriter, r *http.Request) {
	_ = w
	_ = r
}
