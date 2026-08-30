//go:build ignore

package primaryaction

// Decide routes an empty, non-eligible section away from the approve tail with
// a OpenStepVisibleCount == 0 guard before the terminal KindConfirm return.
func Decide(in Input) Action {
	if in.OpenStepVisibleCount == 0 && !in.SectionReady {
		return resolveFinishAction(in)
	}
	if in.SectionDone {
		return resolveFinishAction(in)
	}
	return Action{Kind: KindConfirm}
}
