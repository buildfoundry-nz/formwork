//go:build ignore

package scaleapply

// The write was removed. It used to be `m["appliedScale"] = scale` here; the
// key now exists only in this comment and decodes back to 0.
func composeAdjustmentReference(m map[string]any, cal string) {
	m["appliedCalibration"] = cal
}
