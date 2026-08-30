//go:build ignore

package stocktemplates

// buildKey open-codes the native selection-key literal instead of funnelling
// through skuselections.FormatChoiceKey — the #1436 drift hazard.
func buildKey(sel, opt string) string {
	return "vk:" + sel + ":" + opt // want: sku-selection-key-construction
}
