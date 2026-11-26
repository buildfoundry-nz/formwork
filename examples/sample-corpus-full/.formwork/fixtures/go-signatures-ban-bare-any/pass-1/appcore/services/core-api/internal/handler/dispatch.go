//go:build ignore

package handler

type Config struct {
	Name string
	Size int
}

func Handle(x string) int {
	return len(x)
}

func Ident[T comparable](v T) T {
	return v
}
