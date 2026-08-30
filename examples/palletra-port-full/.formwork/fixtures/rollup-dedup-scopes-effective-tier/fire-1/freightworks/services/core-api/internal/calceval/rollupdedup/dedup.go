//go:build ignore

package rollupdedup

// rollupKey folds rows by building unit only (the NULL no-op).
func rollupKey(r Row) string {
	return r.FacilityUnitID
}
