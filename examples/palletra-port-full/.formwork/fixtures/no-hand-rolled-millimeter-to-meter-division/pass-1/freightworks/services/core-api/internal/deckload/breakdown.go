//go:build ignore

package deckload

import "github.com/palletra/freightworks/services/core-api/internal/scalecalc"

// PartitionHeightMeters routes the mm->m conversion through the canonical primitive.
func PartitionHeightMeters(spanMM float64) float64 {
	// A doc comment spelling the formula spanMM / 1000 must not fire.
	return scalecalc.MetersFromMillimeters(spanMM)
}

// RebarTons is a kg->tons mass conversion (E2), not a length divide.
func RebarTons(spanM, kgOverM float64) float64 {
	out := 0.0
	out += spanM * kgOverM / 1000
	return out
}

// SnapRatio is the fixed-decimal rounding round-trip (E3), not a unit conv.
func SnapRatio(r float64) float64 {
	return float64(int(r*1000+0.5)) / 1000
}
