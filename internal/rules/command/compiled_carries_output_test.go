package command

import "testing"

// rebuild replays a rule the way fixture compile-once does and returns the
// concrete rule back, failing the test rather than panicking if the rebuild
// handed back something else.
func rebuild(t *testing.T, c *command) *command {
	t.Helper()
	out, ok := WithCompiledBinary(c, "/tmp/built", nil, "")
	if !ok {
		t.Fatal("WithCompiledBinary refused a command rule")
	}
	got, ok := out.(*command)
	if !ok {
		t.Fatalf("WithCompiledBinary returned %T, want *command", out)
	}
	return got
}

// WithCompiledBinary rebuilds the rule for fixture replay, and every
// declaration the author made has to survive that rebuild.
//
// It cannot copy the struct wholesale — atomic.Bool fields make `*orig` a
// vet-refused lock copy — so it lists the fields by hand, and a field added to
// the rule and not to that list is dropped SILENTLY. That is how
// params.output went missing on the fixture path while working perfectly under
// check: the corpus declared findings-v1, `formwork test` replayed the rule
// with the declaration stripped, and the fixture saw one pathless blob.
//
// A declaration that quietly does nothing is the exact defect the output
// contract exists to remove, so it must not be reachable through this door.
func TestWithCompiledBinary_CarriesTheOutputDeclaration(t *testing.T) {
	if !rebuild(t, &command{cmd: []string{"detector"}, located: true}).located {
		t.Fatal("the rebuilt rule lost params.output — fixture replay would report one pathless blob for a rule whose corpus declares locations, and nothing would say so")
	}
}

// The mirror: a rule that declared nothing must not acquire the contract by
// passing through the rebuild.
func TestWithCompiledBinary_DoesNotInventTheOutputDeclaration(t *testing.T) {
	if rebuild(t, &command{cmd: []string{"detector"}}).located {
		t.Fatal("the rebuilt rule gained a declaration its corpus never made")
	}
}
