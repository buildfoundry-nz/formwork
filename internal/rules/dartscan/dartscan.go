// Package dartscan implements heuristic analyzers for Dart source. There is no
// Go Dart parser available, so these rules operate purely on lines and a naive
// brace-depth count (+{ -}) to approximate method bodies. This is a heuristic:
// braces, `await`, and access/guard tokens appearing inside string literals or
// comments are not distinguished from real code and can cause false positives
// or false negatives. Files whose name does not end in `.dart` yield no
// findings. A method body whose braces never balance before end-of-file is
// treated as a parse failure and returned as an error (engine exit 2), never a
// silent pass.
package dartscan

import (
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// isDart reports whether f is a Dart source file by extension.
func isDart(f *scan.File) bool {
	return strings.HasSuffix(f.Path(), ".dart")
}

// braceDelta is the net change in brace depth contributed by a line.
func braceDelta(line string) int {
	return strings.Count(line, "{") - strings.Count(line, "}")
}

// isAbstractOrArrow reports whether a matched header line declares no
// brace-delimited body (an abstract declaration or `=>` shorthand): it ends a
// statement with `;` and opens no `{`. Such headers are skipped so body-tracked
// rules do not flag declarations that have no block body.
func isAbstractOrArrow(line string) bool {
	return !strings.Contains(line, "{") && strings.Contains(line, ";")
}
