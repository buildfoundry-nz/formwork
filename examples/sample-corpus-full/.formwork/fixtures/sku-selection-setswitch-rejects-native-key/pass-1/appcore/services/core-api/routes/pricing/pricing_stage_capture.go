//go:build ignore

package pricing

import "github.com/sample/appcore/services/core-api/internal/skuselections/selkeys"

// SetToggle writes a Kit K-code switch, rejecting native sel: keys.
func SetToggle(key, val string) error {
	if selkeys.IsSourceKey(key) {
		return errInvalidNamespace
	}
	store[key] = val
	return nil
}
