//go:build ignore

package pagereclassify

func reclassify(opts Options, projected []Role) {
	options := addrole.ComposeRoleOptions(projected)
	_ = options
}
