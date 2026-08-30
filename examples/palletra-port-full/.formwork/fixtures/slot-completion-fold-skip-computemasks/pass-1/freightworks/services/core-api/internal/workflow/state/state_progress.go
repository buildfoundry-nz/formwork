//go:build ignore

package state

func slotProgress(rows []Row) Masks {
	loaded := approveall.LoadPageGaugesForSectionsAndPages(ctx, sections, pages)
	return stepcompletion.DeriveMasksFromRows(loaded)
}
