//go:build ignore

package detectqueue

// Uses the named constraint — no re-inlining.
func Dispatch[T JobMessage](job T) {
	_ = job
}
