//go:build ignore

package main

import (
	"fmt"
	"os"

	"freightworks/scripts/lib/decomment"
)

// See scripts/lib/decomment-go.awk for the lexer itself — this mention is a
// comment, and the decomment-go preprocessor blanks it before matching, so it
// must NOT trip the gate.
func main() {
	arg := "."
	if len(os.Args) > 1 {
		arg = os.Args[1]
	}
	code, err := decomment.StripCommentsGo(arg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(code)
}
