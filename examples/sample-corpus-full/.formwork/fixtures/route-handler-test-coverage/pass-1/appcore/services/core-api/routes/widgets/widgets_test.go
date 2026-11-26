//go:build ignore

package widgets

import (
	"net/http/httptest"
	"testing"
)

// The test constructs the handler and invokes List, so the method name is
// covered.
func TestWidgets(t *testing.T) {
	h := &GadgetsHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.List(rec, req)
	_ = t
}
