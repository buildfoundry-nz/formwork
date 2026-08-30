// Package preprocess provides line-preserving content transforms that rules
// select via the envelope's preprocess: param (spec §5, §6; phase-3a design
// §1). Transforms know nothing about rules; scan caches their output per
// file. Every Transform MUST be pure and line-preserving: same line count as
// the input, removed text replaced by spaces.
package preprocess

import (
	"fmt"
	"sort"
)

// Transform rewrites content without changing its line structure (enforced
// by scan.File.Variant, which errors if the newline count changes).
//
// A Transform MUST NOT WRITE TO ITS ARGUMENT. Copy first — every implementation
// here opens with `out := append([]byte(nil), src...)` — and return the copy.
// The input is scan's cached content for that file, shared by every rule in the
// run, so an in-place edit would corrupt what every other rule sees. It would
// also silently change strings already returned by scan.File.Lines, which alias
// that same memory rather than copying it (#66). "Pure" in the package doc
// means this; it is stated separately because it is the one part a new
// transform can violate while still passing the line-count check.
type Transform func([]byte) []byte

// registry is written only by Register during package init and read-only
// afterwards, so it needs no synchronization (same discipline as the rule
// registry). Do not call Register after program startup.
var registry = map[string]Transform{}

// Register adds a named transform. Duplicate registration is a programming
// error.
func Register(name string, t Transform) {
	if _, dup := registry[name]; dup {
		panic(fmt.Sprintf("preprocess: duplicate registration of %q", name))
	}
	registry[name] = t
}

// Lookup resolves a transform name. The names "raw" and "" mean no
// transform: callers get (nil, true) and use the original content.
func Lookup(name string) (Transform, bool) {
	if name == "" || name == "raw" {
		return nil, true
	}
	t, ok := registry[name]
	return t, ok
}

// Names lists the accepted preprocess values, sorted, for error messages.
func Names() []string {
	names := []string{"raw"}
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
