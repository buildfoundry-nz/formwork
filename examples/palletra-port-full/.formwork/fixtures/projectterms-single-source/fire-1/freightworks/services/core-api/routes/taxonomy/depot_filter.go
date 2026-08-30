//go:build ignore

package taxonomy

// A second enumeration of the depot and access_grade members, re-typed in a
// switch. This is the drift #3365 closes: the vocabulary now has two owners and
// only one of them is the source of truth.
func depotRegion(depot string) string {
	switch depot {
	case "Northgate": // want: projectterms-single-source
		return "north"
	case "Southpoint": // want: projectterms-single-source
		return "south"
	}
	return ""
}

func gradeWeight(grade string) int {
	if grade == "Racked/Tight" { // want: projectterms-single-source
		return 2
	}
	return 1
}
