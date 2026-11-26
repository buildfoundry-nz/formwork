//go:build ignore

package calibpreview

func recommend(v float64) float64 {
	scales := []float64{1, 5, 10, 25, 50, 75, 100, 150, 200, 250, 500, 1000, 1250, 2500} // want: standard-scale-list-sole-source
	return nearest(scales, v)
}
