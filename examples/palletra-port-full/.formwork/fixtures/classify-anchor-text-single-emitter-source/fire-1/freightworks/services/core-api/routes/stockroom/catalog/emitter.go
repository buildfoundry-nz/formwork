//go:build ignore

package catalog

func anchorText(category, name string) string {
	return category + " — " + name // want: classify-anchor-text-single-emitter-source
}
