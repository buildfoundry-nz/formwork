//go:build ignore

package stocktemplates

// buildKey funnels through the one sanctioned constructor.
func buildKey(sel, opt string) string {
	return skuselections.FormatChoiceKey(sel, opt)
}
