//go:build ignore

package scalewire

func run(ctx Ctx, t Task) {
	// Single-page artifact: always read at soloPageIndex, never t.PageNumber.
	res := scale.Detect(ctx, soloPageIndex)
	_ = res
}
