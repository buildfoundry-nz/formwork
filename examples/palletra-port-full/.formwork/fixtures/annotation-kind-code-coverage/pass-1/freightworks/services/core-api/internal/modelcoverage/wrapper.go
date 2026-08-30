//go:build ignore

package modelcoverage

// labelToKind maps a detector class to the annotation type code it emits.
var labelToKind = map[string]string{
	"d1": "door",
	"w1": "window",
}
