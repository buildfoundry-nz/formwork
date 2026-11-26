//go:build ignore

package migrations

import "testing"

// The sanctioned preamble has been deleted; the harness no longer declares it.
func TestSomething(t *testing.T) {
	t.Skip("no migrated-DB connect helper here")
}
