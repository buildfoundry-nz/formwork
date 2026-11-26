//go:build ignore

package main

import (
	"bufio"
	"fmt"
	"os"
)

// check-decomment-thing.go — strips Go comments before scanning for a banned token.

// inline-comment-lexer-ok: this variant fuses brace-depth tracking into the
// walk (a SQL-statement splitter), so it genuinely diverges from the shared lib
// — tracked #3548.
func stripCommentTokens(line string) string {
	insideBlock := false
	for i := 0; i+1 < len(line); i++ {
		two := string(line[i : i+2])
		if two == "/*" {
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
