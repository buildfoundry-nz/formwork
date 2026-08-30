//go:build ignore

// Reproduces check-restore-scope-derives-from-registry.sh (#1114), ported to
// Go, on the fixture tree: the restore handler resolves scope via
// approveall.GetConfig( and never reads a caller-supplied type list, and the
// wire carries segment_code but cannot express a repeated annotation-type
// field. Comments stripped first.
package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	rule1Re = regexp.MustCompile(`(^|[^A-Za-z0-9_])approveall\.GetConfig\(`)
	rule2Re = regexp.MustCompile(`GetMarkerTypes\(\)|\.AnnotationTypes([^A-Za-z0-9_]|$)`)
	msgRe   = regexp.MustCompile(`^message ReinstateSectionAnnotationsRequest[[:space:]]*\{`)
	rule3a  = regexp.MustCompile(`(^|[^A-Za-z0-9_])segment_code[[:space:]]*=`)
	rule3b  = regexp.MustCompile(`^[[:space:]]*repeated[[:space:]]+string[[:space:]]+[A-Za-z0-9_]*annotation_type[A-Za-z0-9_]*[[:space:]]*=`)
)

// stripCommentTokens blanks each line from its first `//` (sed 's://.*::').
func stripCommentTokens(src string) []string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, "//"); idx >= 0 {
			lines[i] = line[:idx]
		}
	}
	return lines
}

func anyMatch(re *regexp.Regexp, lines []string) bool {
	for _, line := range lines {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func main() {
	const h = "freightworks/services/core-api/routes/annotations/markers_restore_by_section.go"
	const p = "schema/proto/palletra/api/v1/markers_service.proto"
	handlerSource, err := os.ReadFile(h)
	if err != nil {
		fmt.Fprintf(os.Stderr, "missing handler %s\n", h)
		os.Exit(1)
	}
	protoSrc, err := os.ReadFile(p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "missing proto %s\n", p)
		os.Exit(1)
	}
	handler := stripCommentTokens(string(handlerSource))
	proto := stripCommentTokens(string(protoSrc))

	fail := 0
	// RULE 1 — scope resolved via the canonical section registry.
	if !anyMatch(rule1Re, handler) {
		fmt.Fprintln(os.Stderr, "handler does not resolve scope via approveall.GetConfig(")
		fail = 1
	}
	// RULE 2 — no caller-supplied annotation type list read.
	if anyMatch(rule2Re, handler) {
		fmt.Fprintln(os.Stderr, "handler reads a caller-supplied annotation type list")
		fail = 1
	}

	// Isolate the ReinstateSectionAnnotationsRequest message body.
	var body []string
	inMsg := false
	for _, line := range proto {
		if msgRe.MatchString(line) {
			inMsg = true
			continue
		}
		if inMsg && strings.HasPrefix(line, "}") {
			inMsg = false
		}
		if inMsg {
			body = append(body, line)
		}
	}
	if len(body) == 0 {
		fmt.Fprintln(os.Stderr, "message ReinstateSectionAnnotationsRequest not found")
		os.Exit(1)
	}
	// RULE 3a — the wire carries the step identity.
	if !anyMatch(rule3a, body) {
		fmt.Fprintln(os.Stderr, "ReinstateSectionAnnotationsRequest has no segment_code")
		fail = 1
	}
	// RULE 3b — the wire cannot express a repeated annotation-type field.
	if anyMatch(rule3b, body) {
		fmt.Fprintln(os.Stderr, "ReinstateSectionAnnotationsRequest carries a repeated annotation-type field")
		fail = 1
	}
	os.Exit(fail)
}
