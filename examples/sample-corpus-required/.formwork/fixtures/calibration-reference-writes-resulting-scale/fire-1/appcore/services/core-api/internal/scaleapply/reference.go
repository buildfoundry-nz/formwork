//go:build ignore

package scaleapply

// The write was removed; appliedScale decodes back to 0.
func composeAdjustmentReference(m map[string]any, cal string) {
	m["appliedCalibration"] = cal
}
