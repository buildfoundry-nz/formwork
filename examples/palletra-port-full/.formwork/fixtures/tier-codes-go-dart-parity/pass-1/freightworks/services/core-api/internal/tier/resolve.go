//go:build ignore

package tier

// Code is the out-of-band integer magnitude for a named tier. Go owns the
// encoding; the Dart tierLabel switch hand-copies these magnitudes.
type Code int

const (
	Unknown   Code = -999
	Ground    Code = 0
	Basement  Code = -1
	Mezzanine Code = 900
	Loft      Code = 901
)
