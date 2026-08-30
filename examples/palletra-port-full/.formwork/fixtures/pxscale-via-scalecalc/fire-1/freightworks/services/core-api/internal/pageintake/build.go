//go:build ignore

package pageintake

// assembleInput assembles a PageIntake. ScaleRatio is meters-per-pixel and here it is
// wrongly assigned the raw pages.scale plan denominator (a doc mention of
// in.ScaleRatio = ... in this comment must not fire).
func assembleInput(in *PageIntake, scale *float64) {
	if in.ScaleRatio == 0 {
		in.ScaleRatio = *scale // want: pxscale-via-scalecalc
	}
}
