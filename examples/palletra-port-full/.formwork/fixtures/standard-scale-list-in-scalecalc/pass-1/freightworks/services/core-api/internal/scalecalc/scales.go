//go:build ignore

package scalecalc

var CommonScales = []float64{1, 5, 10, 20, 25, 50, 75, 100, 150, 200, 250, 500, 1000, 1250, 2500}

func ClosestStandardScale(v float64) float64 { return snap(CommonScales, v) }
