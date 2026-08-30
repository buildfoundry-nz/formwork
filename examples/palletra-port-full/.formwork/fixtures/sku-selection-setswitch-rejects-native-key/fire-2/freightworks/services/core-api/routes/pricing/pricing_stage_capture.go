//go:build ignore

package pricing

// SetToggle writes a Kit K-code switch.
// Regressed: the native-key guard was deleted.
func SetToggle(key, val string) error {
	store[key] = val
	return nil
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// if selkeys.IsSourceKey(key) {
