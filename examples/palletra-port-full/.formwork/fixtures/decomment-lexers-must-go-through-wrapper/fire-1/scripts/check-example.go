//go:build ignore

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	lib := filepath.Join(root, "scripts/lib/decomment-go.awk") // want: decomment-lexers-must-go-through-wrapper
	fmt.Println(lib)
}
