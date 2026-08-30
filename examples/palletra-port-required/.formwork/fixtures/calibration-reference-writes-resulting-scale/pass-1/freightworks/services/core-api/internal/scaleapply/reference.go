//go:build ignore

package scaleapply

func composeAdjustmentReference(m map[string]any, cal string, scale float64) {
	m["appliedCalibration"] = cal
	m["appliedScale"] = scale
}
