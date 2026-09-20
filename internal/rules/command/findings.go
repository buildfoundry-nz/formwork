package command

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/rules"
)

// findingsFormat is the one declared output contract for a command detector,
// named in params.output. A rule that declares nothing keeps today's single
// pathless message, so an existing corpus loads and behaves exactly as before.
const findingsFormat = "findings-v1"

// parseFindings turns a conforming detector's output into structured findings.
//
// The convention is the one every Go tool already speaks — `path: message` or
// `path:line: message` — so a detector adopting it usually only has to print
// what it already knows in a different order.
//
// WHY THE ENGINE AND NOT THE LOADER. The alternative was regexes per detector
// in whatever consumes the output, which is N patterns drifting from N tools:
// the divergence shape this estate refuses on sight. Parsing here means a
// detector either conforms and yields locations or declares that it cannot,
// and the count of rules that cannot is a disclosed number rather than a
// silence.
//
// exists reports whether a repo-relative path is in the tree under
// evaluation. It is the DISCRIMINATOR, not punctuation: plenty of ordinary
// output reads as "word: prose", and minting a finding against a path that is
// not there would send a reader somewhere real and unrelated — strictly worse
// than the pathless finding this replaces.
//
// Nothing is dropped. Lines that are not locations are joined into a single
// pathless finding, because a contract that silently loses a detector's cure
// trades one blindness for another.
func parseFindings(out []byte, exists func(rel string) bool) []rules.Match {
	var located []rules.Match
	var remainder []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if m, ok := parseLocationLine(line, exists); ok {
			located = append(located, m)
			continue
		}
		remainder = append(remainder, line)
	}
	if len(remainder) > 0 {
		located = append(located, rules.Match{Message: strings.Join(remainder, "\n")})
	}
	return located
}

// parseLocationLine reads one `path[:line]: message` line. The path is taken
// only when the tree has it; everything else is the caller's remainder.
func parseLocationLine(line string, exists func(rel string) bool) (rules.Match, bool) {
	head, message, ok := strings.Cut(line, ": ")
	if !ok {
		return rules.Match{}, false
	}
	path, lineNo := head, 0
	// A trailing `:<digits>` is the line number. Split from the RIGHT, so a
	// path that itself contains a colon is not mangled by a greedy read.
	if rest, num, found := cutLast(head, ":"); found && num != "" {
		if n, err := strconv.Atoi(num); err == nil && n > 0 {
			path, lineNo = rest, n
		}
	}
	if !locatable(path, exists) {
		return rules.Match{}, false
	}
	return rules.Match{Path: path, Line: lineNo, Message: message}, true
}

// locatable reports whether path names a file inside the tree under
// evaluation. Absolute paths and anything climbing out are refused before the
// tree is consulted: a location the engine reports must be one a reader can
// open relative to what was scanned.
func locatable(path string, exists func(rel string) bool) bool {
	if path == "" || strings.ContainsAny(path, " \t") {
		return false
	}
	if strings.HasPrefix(path, "/") || filepath.IsAbs(path) {
		return false
	}
	for _, seg := range strings.Split(filepath.ToSlash(path), "/") {
		if seg == ".." {
			return false
		}
	}
	return exists(path)
}

// cutLast is strings.Cut from the right-hand end.
func cutLast(s, sep string) (before, after string, found bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}
