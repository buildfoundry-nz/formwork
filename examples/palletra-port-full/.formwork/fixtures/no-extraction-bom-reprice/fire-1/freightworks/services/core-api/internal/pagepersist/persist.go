//go:build ignore

package pagepersist

// Option configures the page persister.
type Option func(*persister)

// re-introduces the removed per-SavePage re-price seam.
func WithPostCommit(fn scoreHook) Option { // want: no-extraction-bom-reprice
	return func(p *persister) { p.postRunHook = fn }
}
