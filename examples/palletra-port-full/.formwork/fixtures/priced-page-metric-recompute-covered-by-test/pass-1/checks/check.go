//go:build ignore

// Reproduction of check-priced-page-metric-recompute-covered-by-test.sh, ported
// to Go, on the fixture tree: derive the priced-code set from the approve
// configs, then require every code to be covered by a real recompute
// integration test.
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

var (
	driveRe      = regexp.MustCompile(`Recompute\(|approve-step|ApproveStage`)
	readPattern  = regexp.MustCompile(`page_gauges|aggregatedGaugeByCode|readPricedPageGauge`)
	tagRe        = regexp.MustCompile(`(?m)^//go:build integration`)
	codeShapeRe  = regexp.MustCompile(`^[a-z0-9_]+$`)
	readCallTmpl = `(aggregatedGaugeByCode|readPricedPageGauge)\([^)]{0,250}["']%s["']`
	codeEqTmpl   = `code[[:space:]]*=[[:space:]]*'%s'`
)

func main() {
	const approveDir = "freightworks/services/core-api/internal/workflow/approve"
	if info, err := os.Stat(approveDir); err != nil || !info.IsDir() {
		fmt.Fprintln(os.Stderr, "no approve dir")
		os.Exit(2)
	}

	// 1. Derive priced codes from PageTallyCodes blocks in approve/*.go.
	files, err := filepath.Glob(filepath.Join(approveDir, "*.go"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "no approve dir")
		os.Exit(2)
	}
	sort.Strings(files)
	pageCodeSet := map[string]bool{}
	capturing := false
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "PageTallyCodes:") {
				capturing = true
			}
			if capturing {
				parts := strings.Split(line, `"`)
				for i := 1; i < len(parts); i += 2 {
					if codeShapeRe.MatchString(parts[i]) {
						pageCodeSet[parts[i]] = true
					}
				}
				if strings.Contains(line, "}") {
					capturing = false
				}
			}
		}
	}
	priced := make([]string, 0, len(pageCodeSet))
	for c := range pageCodeSet {
		priced = append(priced, c)
	}
	sort.Strings(priced)
	if len(priced) == 0 {
		fmt.Fprintln(os.Stderr, "no priced codes derived")
		os.Exit(1)
	}

	// 2. Corpus: integration tests that BOTH drive recompute AND read page_gauges.
	var corpus []string
	filepath.WalkDir("freightworks", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		corpus = append(corpus, path)
		return nil
	})
	sort.Strings(corpus)
	var flat strings.Builder
	for _, f := range corpus {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		src := string(data)
		if !tagRe.MatchString(src) {
			continue
		}
		if !driveRe.MatchString(src) || !readPattern.MatchString(src) {
			continue
		}
		flat.WriteString(" ")
		flat.WriteString(strings.ReplaceAll(src, "\n", " "))
	}
	flatStr := flat.String()

	// 3. Every priced code must be BOUND to a page_gauges read in the corpus.
	rc := 0
	for _, code := range priced {
		q := regexp.QuoteMeta(code)
		if regexp.MustCompile(fmt.Sprintf(readCallTmpl, q)).MatchString(flatStr) {
			continue
		}
		if regexp.MustCompile(fmt.Sprintf(codeEqTmpl, q)).MatchString(flatStr) {
			continue
		}
		fmt.Fprintf(os.Stderr, "uncovered priced page metric: %s\n", code)
		rc = 1
	}
	os.Exit(rc)
}
