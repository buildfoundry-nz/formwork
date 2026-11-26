//go:build ignore

package commit

// rolloverAnchor is a divergent second emitter of the subcategory-anchor text.
func rolloverAnchor(category, name string) string {
	return category + " — " + name // want: only-one-classify-anchor-text-emitter
}
