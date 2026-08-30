//go:build ignore

package members

// UpdateGrade writes native_role from the request-derived role WITHOUT a
// role-rank ceiling, so a delegated non-admin can mint a role above its own
// tier (sweep-2 #5 vertical privesc).
func UpdateGrade(req *Request, claims *Claims, tx Tx) error {
	role := req.GetBuiltinGrade() // want: assigned-role-capped-by-caller-ceiling
	_, err := tx.Exec(`UPDATE memberships SET native_role = $1 WHERE id = $2`, role, req.MemberID)
	return err
}
