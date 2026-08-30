package pairconsistency

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Dart unit extraction, added for the validating port. There is no
// Go Dart parser, so — exactly like the dart/* analyzers in
// internal/rules/dartscan — units are found by a brace-depth walk over
// lines, and braces inside string literals or comments are
// indistinguishable from code. A unit opens when a
// declaration-scope header that is neither a container (class / mixin /
// enum / extension) nor a control-flow opener starts a brace body, and it
// closes when the depth returns to the opening depth. Multi-line signatures
// accumulate into the pending header so they cannot blind the unit (the
// Dart analogue of #9767). Arrow-bodied members have no brace body and are
// not units; collection-literal initializers open blocks, not units — both
// residues are pinned by tests and disclosed in spec §5.

var (
	// dartContainerHead matches a container declaration, with any modifier
	// run (`abstract class`, `sealed class`, `mixin class`, `extension type`).
	// The trailing \s keeps identifiers that merely START with a keyword
	// (`classificationReport`) from matching.
	dartContainerHead = regexp.MustCompile(`^(?:[\w@]+\s+)*?(?:class|mixin|enum|extension)\s`)
	// dartControlHead rejects control-flow openers that can appear where a
	// unit header would otherwise be read (top-level code in a script-style
	// test file, or a misbalanced body the walk has not entered).
	dartControlHead = regexp.MustCompile(`^(?:if|for|while|switch|try|else|catch|on|do|finally|return|throw|assert|yield|rethrow)\b`)
	dartIdent       = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
)

// dartFuncUnits returns the function-grain units of one .dart file. A unit
// left open at EOF means the braces never balanced — a parse failure
// (engine error), never a silent skip, mirroring dartscan's contract.
func dartFuncUnits(path string, content []byte) ([]funcUnit, error) {
	var units []funcUnit
	depth := 0
	inUnit := false
	openDepth := 0
	unit := funcUnit{}
	var pending strings.Builder
	pendingStart := 0
	pendingLine := 0

	offset := 0
	for i, raw := range strings.SplitAfter(string(content), "\n") {
		line := strings.TrimSuffix(raw, "\n")
		lineNo := i + 1
		delta := strings.Count(line, "{") - strings.Count(line, "}")

		if inUnit {
			depth += delta
			if depth <= openDepth {
				unit.end = offset + len(raw)
				units = append(units, unit)
				inUnit = false
			}
			offset += len(raw)
			continue
		}

		open := strings.Index(line, "{")
		if open == -1 {
			depth += delta
			switch {
			case delta != 0 || strings.Contains(line, ";"):
				// A `}`-carrying line or a completed statement ends any
				// accumulated signature (abstract/arrow declarations land
				// here — they open no body and are never units).
				pending.Reset()
			case strings.TrimSpace(line) != "":
				if pending.Len() == 0 {
					pendingStart = offset
					pendingLine = lineNo
				}
				pending.WriteString(strings.TrimSpace(line) + " ")
			}
			offset += len(raw)
			continue
		}

		header := pending.String() + strings.TrimSpace(line[:open])
		start, startLine := pendingStart, pendingLine
		if pending.Len() == 0 {
			// No accumulated signature — the unit starts at this line.
			start, startLine = offset, lineNo
		}
		pending.Reset()
		if isDartUnitHeader(header) {
			unit = funcUnit{name: dartUnitName(header), start: start, line: startLine}
			inUnit = true
			openDepth = depth
		}
		depth += delta
		if inUnit && depth <= openDepth {
			// Opened and closed on one line (`int twice(int x) => …` has no
			// braces; this is `void f() { g(); }`).
			unit.end = offset + len(raw)
			units = append(units, unit)
			inUnit = false
		}
		offset += len(raw)
	}

	if inUnit {
		return nil, fmt.Errorf("pair-consistency same-func: %s: unterminated function body %q starting at line %d (braces never balanced)",
			path, unit.name, unit.line)
	}
	return units, nil
}

// isDartUnitHeader reports whether the text before a `{` declares a
// function-shaped body rather than a container, a control-flow opener, or a
// literal. The positive test is the last character: a header ends in `)`
// (parameter list) or an identifier (getters, `async`), where a collection
// literal ends in `=`, `(`, `,`, `[` or `:`.
func isDartUnitHeader(header string) bool {
	h := strings.TrimSpace(header)
	if h == "" {
		return false
	}
	if dartContainerHead.MatchString(h) || dartControlHead.MatchString(h) {
		return false
	}
	last := rune(h[len(h)-1])
	return last == ')' || last == '_' || unicode.IsLetter(last) || unicode.IsDigit(last)
}

// dartUnitName derives the display name from an accumulated header: the last
// identifier before the first `(` — `void write()` → write, `final write =
// ()` → write, `String get label` (no paren) → label. A parenthesised
// header with no preceding identifier is an anonymous closure.
func dartUnitName(header string) string {
	h := strings.TrimSpace(header)
	if cut := strings.IndexByte(h, '('); cut != -1 {
		h = h[:cut]
	}
	ids := dartIdent.FindAllString(h, -1)
	if len(ids) == 0 {
		return "<closure>"
	}
	return ids[len(ids)-1]
}
