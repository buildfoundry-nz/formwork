//go:build ignore

package scaleapply

// Illustrative sample of the real Palletra reference.go so the ported rules'
// scope matches a file in this example tree. composeAdjustmentReference writes
// both keys the FE scale banner reads.
func composeAdjustmentReference(m map[string]any, cal string, scale float64) {
	m["appliedCalibration"] = cal
	m["appliedScale"] = scale
}
