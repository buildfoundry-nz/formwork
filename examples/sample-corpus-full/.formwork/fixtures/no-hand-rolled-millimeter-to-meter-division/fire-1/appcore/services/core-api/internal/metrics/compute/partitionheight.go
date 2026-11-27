//go:build ignore

package compute

// PartitionHeightMeters converts a captured partition height in millimeters to meters.
func PartitionHeightMeters(spanMM float64) float64 {
	meters := spanMM / 1000 // want: no-hand-rolled-millimeter-to-meter-division
	return meters
}
