//go:build ignore

package partitionwidth

func projectFactor(px, py, x1, y1, dx, dy, segLenSq float64) float64 {
	t := ((px-x1)*dx + (py-y1)*dy) / segLenSq // want: no-reimplemented-point-to-segment-math
	return t
}
