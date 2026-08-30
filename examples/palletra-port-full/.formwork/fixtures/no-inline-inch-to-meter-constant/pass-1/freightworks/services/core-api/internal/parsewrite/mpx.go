//go:build ignore

package parsewrite

import "github.com/palletra/freightworks/services/core-api/internal/scalecalc"

// mpx = (0.0254 / dpi) * drawingScale * calibration — the formula now lives in
// scalecalc. This doc comment spells the constant and must not trip the gate.
func deriveScaleFactor(dpi, scale, cal float64) float64 {
	return scalecalc.UnitsPerPixel(dpi, scale, cal)
}
