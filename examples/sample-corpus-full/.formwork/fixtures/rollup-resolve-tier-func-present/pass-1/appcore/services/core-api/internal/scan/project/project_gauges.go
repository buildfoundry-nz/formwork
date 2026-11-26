//go:build ignore

package project

// ResolveTier backs EffectiveTier off the tier resolver.
func ResolveTier(p Page) string {
	if p.ManualTier != "" {
		return p.ManualTier
	}
	return p.tierLevel
}
