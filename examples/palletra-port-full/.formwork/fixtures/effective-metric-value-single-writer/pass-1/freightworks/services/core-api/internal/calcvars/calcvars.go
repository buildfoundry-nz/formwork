//go:build ignore

package calcvars

func effectiveValue(m ProjectTally) float64 {
	if m.ManualOverride != nil {
		return *m.ManualOverride
	}
	return m.GetValue()
}

func Build(m ProjectTally) map[string]float64 {
	total := effectiveValue(m)
	return map[string]float64{"total": total}
}
