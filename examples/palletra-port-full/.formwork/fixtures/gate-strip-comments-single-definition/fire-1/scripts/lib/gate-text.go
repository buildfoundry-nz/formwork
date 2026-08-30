//go:build ignore

// gate-text.go — the canonical home for the shared dropComments primitive.
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
