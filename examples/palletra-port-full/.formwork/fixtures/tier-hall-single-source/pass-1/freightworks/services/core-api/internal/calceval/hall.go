//go:build ignore

package calceval

type unit struct {
	FacilityUnitID *facilityUnit
}

type facilityUnit struct {
	HallKey string
}

func hallKey(u *unit, title string) string {
	return tierscope.Hall(u.FacilityUnitID, titleHallKey(title))
}
