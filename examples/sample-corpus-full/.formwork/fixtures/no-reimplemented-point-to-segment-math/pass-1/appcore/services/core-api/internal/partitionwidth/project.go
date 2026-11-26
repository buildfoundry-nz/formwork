//go:build ignore

package partitionwidth

func projectFactor(px, py, x1, y1, x2, y2 float64) float64 {
	t, _ := geom.NearestPointOnSegment(px, py, x1, y1, x2, y2)
	return geom.Clamp01(t)
}
