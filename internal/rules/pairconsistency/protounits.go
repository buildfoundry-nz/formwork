package pairconsistency

import (
	"fmt"
	"regexp"
	"strings"
)

// Proto unit extraction, added for the validating port. No Go proto
// parser is vendored, so — like the Dart extractor — units are found by a
// brace-depth walk over lines. A unit opens at a message or enum block, or
// at an rpc body block, and closes when the depth returns to the opening
// depth. A service block
// is a SCOPE, not a unit — the same shape as a Dart class — so each rpc
// body owes its own companion. Units do not nest otherwise: a nested
// message's fields belong to the enclosing message's unit, mirroring how
// the Go mode folds a nested func literal into its enclosing function's
// span. Declarations outside any unit (a file-level `option`, service-level
// options) belong to no unit — the proto mode's disclosed residue, pinned
// by test. The finding names the unit with its keyword ("message Foo")
// because "func Foo" would misdescribe a schema block.

// protoUnitHead matches the declarations that open a unit. `service` is a
// scope (its rpc bodies are the units); `oneof`, `extend` and option blocks
// are neither — their contents belong to the enclosing unit (or to no unit
// at file scope), not to a grain the rule language has a name for.
var protoUnitHead = regexp.MustCompile(`^\s*(message|enum|rpc)\s+([A-Za-z_][A-Za-z0-9_]*)`)

// protoFuncUnits returns the block-grain units of one .proto file. A unit
// left open at EOF means the braces never balanced — a parse failure
// (engine error), never a silent skip.
func protoFuncUnits(path string, content []byte) ([]funcUnit, error) {
	var units []funcUnit
	depth := 0
	inUnit := false
	openDepth := 0
	unit := funcUnit{}

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

		if m := protoUnitHead.FindStringSubmatch(line); m != nil && strings.Contains(line, "{") {
			unit = funcUnit{name: m[1] + " " + m[2], start: offset, line: lineNo}
			inUnit = true
			openDepth = depth
		}
		depth += delta
		if inUnit && depth <= openDepth {
			unit.end = offset + len(raw)
			units = append(units, unit)
			inUnit = false
		}
		offset += len(raw)
	}

	if inUnit {
		return nil, fmt.Errorf("pair-consistency same-func: %s: unterminated block %q starting at line %d (braces never balanced)",
			path, unit.name, unit.line)
	}
	return units, nil
}
