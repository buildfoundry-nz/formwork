package cli

import (
	"strings"
	"testing"
)

// emitJSON refuses a value that would render as anything but an object.
func TestEmitJSONRefusesABareList(t *testing.T) {
	var out, errOut strings.Builder
	if code := emitJSON(&out, &errOut, []string{"a"}); code != 2 || out.Len() != 0 || !strings.Contains(errOut.String(), "not a document") {
		t.Fatalf("emitJSON(slice) = %d, stdout %q, stderr %q; want 2, nothing, and the refusal", code, out.String(), errOut.String())
	}
}
