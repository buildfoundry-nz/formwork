// countgrain_test.go — the counting grain of the countable-obligation arm.
//
// Per line, one matching line counts ONCE however many times the pattern occurs
// on it. Unifying the matcher so pair-consistency could accept `syntax` and
// `multiline` also moved counting from "matching lines" to "occurrences", and
// that silently broke a correct pass fixture in an adopting corpus:
//
//	a AS (... ORDER BY (x IS NOT NULL) DESC, elapsed_at DESC, occurred_at DESC, CASE ... , id ASC),
//	b AS (... ORDER BY (y IS NOT NULL) DESC, error_code_at DESC, occurred_at DESC, CASE ... , id ASC)
//	ORDER BY project_id, stage, event_kind, occurred_at DESC, id ASC
//
// Three ORDER BY clauses, each carrying the canonical tail. The trigger
// `[a-z_]+_at DESC` OCCURS five times because `elapsed_at` and `error_code_at`
// are secondary sort keys inside a clause, not separate orderings owing their
// own tiebreak — so occurrence counting read 5 triggers against 3 companions
// and fired on a file that satisfies the invariant. The rule had not changed
// and neither had the tree.
//
// No existing unit test caught it because seeing it needs a line with TWO
// trigger matches, and every fixture here had one per line.
package pairconsistency_test

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

const countGrainParams = `
where: same-file
trigger: '[a-z_]+_at DESC'
requires: 'occurred_at DESC, id ASC'
obligation: countable
`

// Two trigger occurrences on each of two lines, one companion per line. Per
// line that is 2 against 2 — satisfied. Per occurrence it is 4 against 2, and
// a compliant file fires.
func TestCountableCountsMatchingLinesNotOccurrences(t *testing.T) {
	src := "package p\n\n" +
		"const a = `ORDER BY elapsed_at DESC, occurred_at DESC, id ASC`\n" +
		"const b = `ORDER BY error_code_at DESC, occurred_at DESC, id ASC`\n"

	c := mustChecker(t, countGrainParams)
	ms, err := c.CheckFile(scan.NewMemFile("q.go", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("findings = %+v, want none: each triggering LINE carries its companion, so the obligation is met", ms)
	}
}

// The control, so the fix above cannot be "count nothing": a trigger with no
// companion anywhere must still fire.
func TestCountableStillFiresOnAnUnaccompaniedTrigger(t *testing.T) {
	src := "package p\n\nconst a = `ORDER BY elapsed_at DESC`\n"

	c := mustChecker(t, countGrainParams)
	ms, err := c.CheckFile(scan.NewMemFile("q.go", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("findings = %+v, want one: the trigger occurs and no companion does", ms)
	}
}

// And the countable arm still catches a genuine shortfall across LINES: three
// triggering lines, two companions. Per line this is 3 against 2, which is the
// free-ride the arm exists to refuse — so restoring the grain does not blunt it.
func TestCountableStillCatchesAShortfallAcrossLines(t *testing.T) {
	src := "package p\n\n" +
		"const a = `ORDER BY occurred_at DESC, id ASC`\n" +
		"const b = `ORDER BY occurred_at DESC, id ASC`\n" +
		"const c = `ORDER BY elapsed_at DESC`\n"

	ck := mustChecker(t, countGrainParams)
	ms, err := ck.CheckFile(scan.NewMemFile("q.go", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("findings = %+v, want one: 3 triggering lines against 2 companions is a shortfall", ms)
	}
}
