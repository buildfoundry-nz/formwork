//go:build ignore

package pageintake

// assembleInput assembles a PageIntake, deriving ScaleRatio canonically.
func assembleInput(in *PageIntake, drawingScale, resolvedDpi float64, calibration float64) {
	if in.ScaleRatio == 0 {
		in.ScaleRatio = scaleRatioFromPage(drawingScale, resolvedDpi, calibration)
	}
	meta := AttachedPageMeta{ScaleRatio: scalecalc.UnitsPerPixel(drawingScale, resolvedDpi)}
	_ = meta
}
