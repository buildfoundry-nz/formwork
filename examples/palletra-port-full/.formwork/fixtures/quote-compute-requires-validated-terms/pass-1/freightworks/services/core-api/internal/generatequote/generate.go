//go:build ignore

package generatequote

// generate validates the commercial terms before pricing, so an out-of-range
// markup/contingency is rejected before it can write to built_quotes.
func generate(subtotal float64, terms Terms) (float64, error) {
	if err := validatePricingTerms(terms); err != nil {
		return 0, err
	}
	return quoterates.Compute(subtotal, terms.Markup, terms.Contingency), nil
}
