//go:build ignore

package pagefeed

// The presence rule was inlined here instead of a single AssetPresent def.
func StoredBytes(hit Hit) ([]byte, bool) {
	if hit.Len() > 0 {
		return hit.Bytes(), true
	}
	return nil, false
}
