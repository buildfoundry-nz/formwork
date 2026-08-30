//go:build ignore

package rollupdedup

// rollupKey folds rows by the tier axis.
func rollupKey(r Row) string {
	return r.EffectiveTier()
}
