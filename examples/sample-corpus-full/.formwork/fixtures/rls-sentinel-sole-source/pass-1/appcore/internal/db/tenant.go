//go:build ignore

package db

// nullUUID is the all-zeros UUID used as the RLS sentinel for a MISSING
// DIMENSION — one dimension-neutral declaration, the single source.
const nullUUID = "00000000-0000-0000-0000-000000000000"

func WithOrgIso(claims Claims) (string, string) {
	depotID := nullUUID
	userID := nullUUID
	if claims.HasSite() {
		depotID = claims.GetDepotId()
	}
	if claims.HasUser() {
		userID = claims.GetUserId()
	}
	return depotID, userID
}
