//go:build ignore

package skuselections

import "errors"

// ErrNotLocalKey → 400 when an K-code (non-sel:) key reaches Apply.
var ErrNotLocalKey = errors.New("skuselections: option key is not a native sel: key")

// ParseSelectionKey should reject non-native keys, but the guard has been deleted
// while the sentinel var survives — the one-writer-per-store invariant is broken.
func ParseSelectionKey(key string) (string, error) {
	slug, _, ok := parseKey(key)
	if !ok {
		return "", errInvalidKey
	}
	return slug, nil
}
