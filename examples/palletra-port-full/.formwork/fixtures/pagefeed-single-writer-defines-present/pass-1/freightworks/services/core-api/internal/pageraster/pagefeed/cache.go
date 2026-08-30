//go:build ignore

package pagefeed

func AssetPresent(size int64) bool { return size > 0 }

func StoredBytes(hit Hit) ([]byte, bool) {
	if AssetPresent(int64(hit.Len())) {
		return hit.Bytes(), true
	}
	return nil, false
}
