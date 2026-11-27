//go:build ignore

package members

// crewListQuery lists an org's members ordered newest-first but WITHOUT a
// LIMIT, so a large org pulls every member per render (sweep-3 #11).
const crewListQuery = `SELECT m.id, u.name FROM memberships m JOIN users u ON u.id = m.user_id WHERE m.org_id = $1 ORDER BY m.created_at DESC` // want: members-list-query-requires-limit
