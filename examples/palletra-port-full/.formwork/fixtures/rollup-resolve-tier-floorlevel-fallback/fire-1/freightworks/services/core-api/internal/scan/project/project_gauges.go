//go:build ignore

package project

// FIRE: ResolveTier has no extraction floor-level fallback.
func ResolveTier(p Page) string {
	return p.ManualTier
}
