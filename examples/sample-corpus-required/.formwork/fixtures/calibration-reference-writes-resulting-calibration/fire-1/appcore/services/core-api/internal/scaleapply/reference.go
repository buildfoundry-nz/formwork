//go:build ignore

package scaleapply

// The write was removed; appliedCalibration decodes back to 0.
func composeAdjustmentReference(m map[string]any, scale float64) {
	m["appliedScale"] = scale
}
