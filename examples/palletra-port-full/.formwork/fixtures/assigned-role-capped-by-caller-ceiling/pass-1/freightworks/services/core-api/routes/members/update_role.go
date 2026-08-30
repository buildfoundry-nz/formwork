//go:build ignore

package members

// UpdateGrade rejects a requested role that out-ranks the caller before writing
// native_role, via the hoisted single-helper ceiling (sweep-3 #13).
func UpdateGrade(req *Request, claims *Claims, tx Tx) error {
	if !roleWithinCap(req.GetBuiltinGrade(), claims.GetAccessGrade()) {
		return ErrForbidden
	}
	role := req.GetBuiltinGrade()
	_, err := tx.Exec(`UPDATE memberships SET native_role = $1 WHERE id = $2`, role, req.MemberID)
	return err
}
