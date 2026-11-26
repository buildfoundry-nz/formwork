//go:build ignore

package skuselections

import "errors"

// ErrNotLocalKey → 400 when an K-code (non-sel:) key reaches Apply.
var ErrNotLocalKey = errors.New("skuselections: option key is not a native sel: key")

// ParseSelectionKey rejects non-native keys — one writer per store (design §D3).
func ParseSelectionKey(key string) (string, error) {
	slug, native, ok := parseKey(key)
	if !ok {
		return "", errInvalidKey
	}
	if !native {
		return "", ErrNotLocalKey
	}
	return slug, nil
}
