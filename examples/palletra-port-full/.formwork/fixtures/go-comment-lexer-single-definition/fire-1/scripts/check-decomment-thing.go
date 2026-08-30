//go:build ignore

package main

import (
	"bufio"
	"fmt"
	"os"
)

// check-decomment-thing.go — strips Go comments before scanning for a banned token.

// A private re-implementation of the string-aware Go comment lexer, pasted in
// without migrating it onto the shared scripts/lib wrapper and without a
// marker.
func stripCommentTokens(line string) string {
	insideBlock := false
	for i := 0; i+1 < len(line); i++ {
		two := string(line[i : i+2])
		if two == "/*" { // want: go-comment-lexer-single-definition
			insideBlock = true
		}
	}
	if insideBlock {
		return ""
	}
	return line
}

func main() {
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		fmt.Println(stripCommentTokens(sc.Text()))
	}
}
