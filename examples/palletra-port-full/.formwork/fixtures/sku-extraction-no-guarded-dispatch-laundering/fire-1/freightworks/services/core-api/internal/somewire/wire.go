//go:build ignore

package somewire

// launder converts a raw transport into a "guarded" one by hand — bypassing
// skuwire.Guard, the only sanctioned constructor (#3022 RULE 4).
func launder(raw RouteFn) GatedDispatchFn {
	return GatedDispatchFn(raw) // want: sku-extraction-no-guarded-dispatch-laundering
}
