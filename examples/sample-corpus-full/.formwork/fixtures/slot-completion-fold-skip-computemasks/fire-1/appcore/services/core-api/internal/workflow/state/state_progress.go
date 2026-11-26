//go:build ignore

package state

func slotProgress(rows []Row) Masks {
	m := stepcompletion.DeriveMasks(ctx, slot, page) // want: slot-completion-fold-skip-computemasks
	return m
}
