//go:build ignore

package generatequote

// generate prices the quote from raw markup/contingency percentages without
// first rejecting out-of-range terms — a negative percent writes a wrong total.
func generate(subtotal float64, terms Terms) float64 {
	return quoterates.Compute(subtotal, terms.Markup, terms.Contingency) // want: quote-compute-requires-validated-terms
}
