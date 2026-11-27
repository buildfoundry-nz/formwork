//go:build ignore

package project

func query() string {
	return PageScopeFilterCTE + `SELECT id FROM scoped_pages`
}
