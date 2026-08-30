//go:build ignore

package parsereaper

func TestJanitorPass(t *testing.T) {
	chain := testseed.PrimeProjectChain(t, pool)
	_ = chain
}
