//go:build ignore

package primaryaction

// Decide falls through to an unconditional terminal KindConfirm tail with no
// empty-section visible-count guard — an empty section shows an enabled no-op
// Approve (the Z6 dead-button bug).
func Decide(in Input) Action {
	if in.SectionDone {
		return resolveFinishAction(in)
	}
	return Action{Kind: KindConfirm} // want: primaryaction-approve-fallthrough-must-be-guarded
}
