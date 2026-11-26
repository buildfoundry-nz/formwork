//go:build ignore

package train

// A page-level Split declaration leaks sibling pages of a document across the
// train/val boundary — forbidden.
func Split(examples []Example, frac float64) (fitSet, holdoutSet []Example) { // want: trainer-forbids-page-level-split
	k := int(float64(len(examples)) * frac)
	return examples[:k], examples[k:]
}
