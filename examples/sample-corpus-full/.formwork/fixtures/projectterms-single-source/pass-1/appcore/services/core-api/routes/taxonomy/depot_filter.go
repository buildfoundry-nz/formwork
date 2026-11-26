//go:build ignore

package taxonomy

// The same two lookups, delegated. The members "Northgate", "Southpoint" and
// "Racked/Tight" are named here only in prose, which decomment-go blanks, so a
// doc comment about the vocabulary is not itself a second copy of it.
func depotRegion(depot string) string {
	return projectterms.Depot(depot).Region()
}

func gradeWeight(grade string) int {
	return projectterms.AccessGrade(grade).Weight()
}
