//go:build ignore

package db

// nullUUID is the all-zeros UUID used as the RLS sentinel for a MISSING
// DIMENSION.
const nullUUID = "00000000-0000-0000-0000-000000000000"

// nullSiteID is a SECOND handle for the one sentinel — a second literal under a
// second name, free to drift into a cross-tenant leak (#2912).
const nullSiteID = "00000000-0000-0000-0000-000000000000"

func WithOrgIso(claims Claims) (string, string) {
	depotID := nullSiteID
	userID := nullUUID
	return depotID, userID
}
