//go:build ignore

// Reproduces the two central scans of
// check-server-error-boundary-sole-source.sh (sweep-19-#2), ported to Go, on
// the fixture tree: over non-test middleware .go (comments stripped), scan 1
// fires on any comparison vs <pkg>.StatusInternalServerError except the
// canonical IsFailureStatus body line; scan 2 fires on any comparison vs a
// 500/499-class literal, excluding a len()/cap() byte-count magnitude.
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

const bareintLit = `500|499|0[xX]1[fF][34]|0[oO]?76[34]`

var (
	allowedRe   = regexp.MustCompile(`^return status >= http\.StatusInternalServerError$|^return http\.StatusInternalServerError <= status$`)
	boundaryRe  = regexp.MustCompile(`(>=|<=|>|<)[[:space:]]*[A-Za-z_][A-Za-z0-9_]*\.StatusInternalServerError|[A-Za-z_][A-Za-z0-9_]*\.StatusInternalServerError[[:space:]]*(>=|<=|>|<)`)
	bareintRe   = regexp.MustCompile(`(>=|<=|>|<)[[:space:]]*(` + bareintLit + `)([^0-9]|$)|(^|[^0-9])(` + bareintLit + `)[[:space:]]*(>=|<=|>|<)`)
	magnitudeRe = regexp.MustCompile(`(len|cap)\([^)]*\)[[:space:]]*(>=|<=|>|<)[[:space:]]*(` + bareintLit + `)|(` + bareintLit + `)[[:space:]]*(>=|<=|>|<)[[:space:]]*(len|cap)\(`)
)

func main() {
	const dir = "freightworks/services/core-api/middleware"
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

	fail := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var stripped []string
		for _, line := range strings.Split(string(data), "\n") {
			if idx := strings.Index(line, "//"); idx >= 0 {
				line = line[:idx]
			}
			stripped = append(stripped, line)
		}
		// Scan 1 — named-constant class boundary, canonical line exempt.
		for _, line := range stripped {
			if !boundaryRe.MatchString(line) {
				continue
			}
			trimmed := strings.TrimSpace(line)
			if allowedRe.MatchString(trimmed) {
				continue
			}
			fmt.Fprintf(os.Stderr, "%s: %s\n", f, trimmed)
			fail = 1
		}
		// Scan 2 — bare/adjacent 500/499 literal, len()/cap() magnitude excluded.
		for _, line := range stripped {
			if !bareintRe.MatchString(line) {
				continue
			}
			trimmed := strings.TrimSpace(line)
			residue := magnitudeRe.ReplaceAllString(trimmed, "")
			if bareintRe.MatchString(residue) {
				fmt.Fprintf(os.Stderr, "%s: bare-literal 5xx boundary — %s\n", f, trimmed)
				fail = 1
			}
		}
	}
	os.Exit(fail)
}
