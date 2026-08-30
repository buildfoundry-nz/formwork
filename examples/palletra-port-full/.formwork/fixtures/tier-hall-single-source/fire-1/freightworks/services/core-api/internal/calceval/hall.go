//go:build ignore

package calceval

type unit struct {
	FacilityUnitID *facilityUnit
}

type facilityUnit struct {
	HallKey string
}

func hallKey(u *unit, title string) string {
	if u.FacilityUnitID != nil { // want: tier-hall-single-source
		return u.FacilityUnitID.HallKey
	}
	return titleHallKey(title)
}
