//go:build ignore

package widgets

import (
	"net/http/httptest"
	"testing"
)

// The test constructs the handler but only exercises a DIFFERENT method, so
// GadgetsHandler.List has no test invocation anywhere in routes/.
func TestWidgets(t *testing.T) {
	h := &GadgetsHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.Create(rec, req)
	_ = t
}
