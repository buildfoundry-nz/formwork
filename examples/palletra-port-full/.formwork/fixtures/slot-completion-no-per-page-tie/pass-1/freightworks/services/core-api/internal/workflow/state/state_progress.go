//go:build ignore

package state

func slotProgress(rows []Row) Masks {
	pre := LoadProgressInputs(ctx, slots)
	fams := pre.TieFamiliesForPages()
	return fold(fams)
}
