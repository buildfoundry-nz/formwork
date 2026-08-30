//go:build ignore

package handler

func Handle(x any) string { // want: go-signatures-ban-bare-any
	return "ok"
}
