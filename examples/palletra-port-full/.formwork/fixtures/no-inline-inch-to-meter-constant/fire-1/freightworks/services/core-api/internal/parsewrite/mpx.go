//go:build ignore

package parsewrite

// A new open-coded meters-per-pixel, divergent from the canonical primitive.
func deriveScaleFactor(dpi, scale, cal float64) float64 {
	return (0.0254 / dpi) * scale * cal // want: no-inline-inch-to-meter-constant
}
