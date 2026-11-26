//go:build ignore

package somewire

// wire builds a guarded dispatch through skuwire.Guard — the only path
// that can mint a GatedDispatchFn. No explicit conversion.
func wire(raw RouteFn) GatedDispatchFn {
	return skuwire.Guard(raw)
}
