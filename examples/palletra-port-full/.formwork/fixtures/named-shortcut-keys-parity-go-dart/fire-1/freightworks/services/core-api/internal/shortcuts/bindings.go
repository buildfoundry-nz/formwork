//go:build ignore

package shortcuts

// allowedShortcutKeys is the closed named-key grammar the rebind PUT validates
// against. The Dart codec must decode exactly this set.
var allowedShortcutKeys = map[string]bool{
	"Delete":      true,
	"ArrowLeft":   true,
	"BracketLeft": true,
}
