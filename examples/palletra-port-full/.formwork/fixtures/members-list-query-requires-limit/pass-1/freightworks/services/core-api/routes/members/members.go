//go:build ignore

package members

// crewListQuery is keyset-paginated: ordered by (created_at DESC, id DESC)
// and bounded by LIMIT so a large org never pulls every member per render.
const crewListQuery = `SELECT m.id, u.name FROM memberships m JOIN users u ON u.id = m.user_id WHERE m.org_id = $1 ORDER BY m.created_at DESC, m.id DESC LIMIT $2`

// fetchMemberQuery fetches a single membership by id — no ORDER BY, not a list
// query, so it must not trip the bound (the single-row Get carve-out).
const fetchMemberQuery = `SELECT m.id, m.native_role FROM memberships m WHERE m.id = $1`
