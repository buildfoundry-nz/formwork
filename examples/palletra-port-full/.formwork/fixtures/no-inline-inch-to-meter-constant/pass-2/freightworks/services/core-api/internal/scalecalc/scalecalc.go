//go:build ignore

package scalecalc

// inchInMeters is the one place the inch->meter conversion factor is spelled
// out; every other package must call UnitsPerPixel instead of open-coding it.
const inchInMeters = 0.0254

// UnitsPerPixel is the single canonical home of the meters-per-pixel formula.
func UnitsPerPixel(dpi, scale, calFactor float64) float64 {
	if dpi > 0 && scale > 0 && calFactor > 0 {
		return (inchInMeters / dpi) * scale * calFactor
	}
	return 0
}
