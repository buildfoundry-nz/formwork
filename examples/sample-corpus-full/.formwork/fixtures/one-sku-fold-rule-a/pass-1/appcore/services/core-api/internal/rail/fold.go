//go:build ignore

package rail

func PlaceRail(rows []Row) []Row {
	return skugroup.Assign(rows)
}
