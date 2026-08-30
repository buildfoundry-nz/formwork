//go:build ignore

package scalewire

func run(ctx Ctx, t Task) {
	res := scale.Detect(ctx, t.PageNumber) // want: scale-detect-no-literal-page-number
	_ = res
}
