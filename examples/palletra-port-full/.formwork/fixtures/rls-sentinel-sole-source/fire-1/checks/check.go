//go:build ignore

// Reproduces RULE A of check-rls-sentinel-sole-source.sh (#2912), ported
// to Go, on the fixture tree: over non-test .go under freightworks/internal/db/,
// the all-zeros RLS sentinel literal must appear EXACTLY ONCE (comments
// stripped), and on a canonical `nullUUID =` declaration line — no second
// literal, no second name.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const lit = "00000000-0000-0000-0000-000000000000"

var canonRe = regexp.MustCompile(`(^|[^A-Za-z0-9_])nullUUID[[:space:]]*=`)

func main() {
	const dir = "freightworks/internal/db"
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		fmt.Printf("no %s, skipping\n", dir)
		os.Exit(0)
	}

	var files []string
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)

	canonLines := 0
	stray := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if idx := strings.Index(line, "//"); idx >= 0 {
				line = line[:idx]
			}
			if !strings.Contains(line, lit) {
				continue
			}
			if canonRe.MatchString(line) {
				canonLines++
			} else {
				fmt.Fprintf(os.Stderr, "second/inline sentinel literal (not a canonical nullUUID decl): %s: %s\n", f, strings.TrimLeft(line, " \t"))
				stray = 1
			}
		}
	}

	fail := 0
	if canonLines == 0 {
		fmt.Fprintf(os.Stderr, "no canonical sentinel declaration (expected exactly one `const nullUUID = \"%s\"`)\n", lit)
		fail = 1
	} else if canonLines > 1 {
		fmt.Fprintf(os.Stderr, "multiple canonical sentinel declarations (found %d; expected exactly one)\n", canonLines)
		fail = 1
	}
	if stray != 0 {
		fail = 1
	}
	os.Exit(fail)
}
