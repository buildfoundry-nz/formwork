//go:build ignore

package catalog

func anchorText(category, name string) string {
	// emits "<cat>" + " — " + "<name>" only via ComposeSubcategoryAnchorText
	return ComposeSubcategoryAnchorText(category, name)
}
