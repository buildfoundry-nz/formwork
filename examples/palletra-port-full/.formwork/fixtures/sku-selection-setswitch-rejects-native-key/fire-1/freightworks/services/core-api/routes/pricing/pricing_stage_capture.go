//go:build ignore

package pricing

// SetToggle writes a Kit K-code switch.
// Regressed: the native-key guard was deleted.
func SetToggle(key, val string) error {
	store[key] = val
	return nil
}
