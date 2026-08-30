//go:build ignore

package pagereclassify

func reclassify(opts Options, projected []Role) {
	role := addrole.LocateRole(opts.Chosen, projected) // want: canonical-role-gate-single-source
	_ = role
}
