//go:build ignore

// check-member-writes-require-outrank-check.go — VIOLATION: re-introduces a local
// copy of dropComments instead of using scripts/lib/gate-text.go. This is the
// duplicated-primitive entropy sweep-5 #8 removed.
package main

import (
	"bufio"
	"os"
	"strings"
)

func dropComments(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		out = append(out, line)
	}
	return out, sc.Err()
}

func main() {
	_, _ = dropComments(os.Args[1])
}
