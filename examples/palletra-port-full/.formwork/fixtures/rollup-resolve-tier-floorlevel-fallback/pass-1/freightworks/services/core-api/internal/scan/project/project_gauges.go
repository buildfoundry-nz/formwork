//go:build ignore

package project

func ResolveTier(p Page) string {
	if p.ManualTier != "" {
		return p.ManualTier
	}
	return p.tierLevel
}
