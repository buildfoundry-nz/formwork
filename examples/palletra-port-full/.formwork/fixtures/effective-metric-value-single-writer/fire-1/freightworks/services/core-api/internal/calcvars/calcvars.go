//go:build ignore

package calcvars

func Build(m ProjectTally) map[string]float64 {
	total := float64(m.GetValue()) // want: effective-metric-value-single-writer
	return map[string]float64{"total": total}
}
