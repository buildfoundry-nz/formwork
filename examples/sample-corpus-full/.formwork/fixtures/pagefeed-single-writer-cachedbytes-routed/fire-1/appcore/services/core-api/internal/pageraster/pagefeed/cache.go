//go:build ignore

package pagefeed

func AssetPresent(size int64) bool { return size > 0 }

func StoredBytes(hit Hit) ([]byte, bool) {
	if hit.Len() > 0 {
		return hit.Bytes(), true
	}
	return nil, false
}
