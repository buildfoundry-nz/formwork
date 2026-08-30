//go:build ignore

package state

func slotProgress(rows []Row) Masks {
	fams := stepcompletion.TieFamiliesOnPage(ctx, sheetID) // want: slot-completion-no-per-page-tie
	return fold(fams)
}
