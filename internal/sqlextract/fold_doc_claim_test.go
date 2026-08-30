// fold_doc_claim_test.go — #337's disclosure half, at the document the other
// three defer to.
//
// `unseenwrite.go` justifies keeping the outside-appends world by pointing at
// this file's contract block — "the package doc says so" — and #337 quoted that
// justification verbatim as the reason the residue is undisclosed: it admits
// three cases, "conditionally called, never called, or created after the value
// is used", and `run(add)` is none of the three. `locking.go`, the fold spec and
// docs/reference.md have each been brought into line with the tree. The block
// all three are downstream of had not been, and it had drifted further than a
// missing sentence: it named `add()` — the spelling `invoked.go` untracks, which
// emits nothing at all — as the fabrication and the disclosed false positive,
// and never mentioned the spelling that actually fabricates.
//
// That is the #312/#313 shape once more, in the direction that misdirects
// triage: a reader holding the `run(add)` finding is told by the contract that
// this class fires on `add()` instead, and a reader of `add()` is told to expect
// a false positive that cannot occur.
//
// BOTH TESTS FAIL IN BOTH DIRECTIONS, on census_wiring_test.go's precedent —
// closing a gap while the document still described it open is what broke that
// build, and it is the half a behavioural test cannot hold. The claims are
// checked against a MEASUREMENT taken through `FromGoReassembled` in this run,
// never against a remembered verdict, so a change to `invoked.go` that moves
// either spelling reddens here until the contract is moved with it.
//
// The known limit of any prose check, recorded because locking_specclaim_test.go
// carries the same one: it matches an assertion, not its negation. A sentence
// that says a shape is NOT a false positive reads to foldDocFabricationRE like
// one that says it is. The vocabulary is named in every failure message so that
// an editor who trips it can see what was matched and why.
package sqlextract_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/sqlextract"
)

// foldDocContractAnchor opens the block fold.go itself calls "the contract for
// everything below it". Anchored on the heading rather than on a line number so
// that an edit which moves the block keeps this test pointed at it, and an edit
// which deletes the heading fails the premise instead of passing vacuously.
const foldDocContractAnchor = "// THE ASSIGNMENT-FLOW FOLD (#36)."

// foldDocEscapeCall and foldDocBareCall are the two ways a named closure can be
// told to run, and the whole subject of #337: one the pass can see, one it
// cannot. They are the exact call text compiled and measured by
// foldDocFabricates, so what is searched for in the prose is what was run.
const (
	foldDocBareCall   = "add()"
	foldDocEscapeCall = "run(add)"
)

// foldDocSeed is the query both programs build, and is deliberately a locking
// SELECT with the ORDER BY inside the closure: that is the composition
// sql/locking-select-order reports on, so a world emitted here without the
// closure's append is the finding #337 is about.
const foldDocSeed = "SELECT id FROM t WHERE s = 'x'"

// foldDocFabricates reports whether this call spelling leaves the fold emitting
// a world built on the seed WITHOUT the closure's append — a value no execution
// path produces, which fold.go's §4.1 contract promises never to emit.
//
// It fails rather than answering false when the run produced no seed candidate
// at all: "the pass untracked q" and "the harness handed it something it could
// not read" are the two outcomes this file exists to tell apart, and they are
// otherwise the same empty result.
func foldDocFabricates(t *testing.T, call string) bool {
	t.Helper()
	src := "package db\n\nfunc run(f func()) { f() }\n\nfunc load() string {\n" +
		"\tq := `" + foldDocSeed + "`\n" +
		"\tadd := func() { q += \" ORDER BY id\" }\n" +
		"\t" + call + "\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	got, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("reassembling the %s program: %v", call, err)
	}
	seedSeen, fabricated := false, false
	for _, c := range got {
		switch {
		case c.Text == foldDocSeed:
			seedSeen = true
		case strings.HasPrefix(c.Text, foldDocSeed):
			fabricated = true
		}
	}
	if !seedSeen {
		t.Fatalf("the expression walk emitted no candidate equal to the seed %q for "+
			"the %s program, so this source was not read at all — an answer of "+
			"\"does not fabricate\" from it would be an artefact of the harness, not "+
			"a measurement of the pass", foldDocSeed, call)
	}
	return fabricated
}

// foldDocContract returns that block with the comment markers stripped, so a
// paragraph in it compares as prose.
func foldDocContract(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("fold.go")
	if err != nil {
		t.Fatalf("fold.go holds the block unseenwrite.go defers to as \"the package "+
			"doc\", and this test cannot check a document it cannot read: %v", err)
	}
	lines := strings.Split(string(b), "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, foldDocContractAnchor) {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("no line in fold.go begins %q: that heading opens the block this "+
			"test checks, so a run that cannot find it is checking nothing",
			foldDocContractAnchor)
	}
	var out []string
	for _, l := range lines[start:] {
		if !strings.HasPrefix(l, "//") {
			break
		}
		out = append(out, strings.TrimPrefix(strings.TrimPrefix(l, "//"), " "))
	}
	if len(out) < 40 {
		t.Fatalf("the contract block read as %d line(s); it is the file's longest "+
			"comment and runs to dozens, so a read that short has stopped at a "+
			"blank-looking line rather than at the end of the block", len(out))
	}
	return strings.Join(out, "\n")
}

// foldDocSentences flattens the block and splits it into sentences.
//
// SENTENCES AND NOT PARAGRAPHS, because the correction this file drove puts
// both spellings in one paragraph: the bare call is untracked and the escaped
// name fabricates, and those two facts belong next to each other. A
// paragraph-scoped match would read the second sentence's verb onto the first
// one's subject and pass on a contract that said the opposite of the tree.
func foldDocSentences(contract string) []string {
	flat := strings.Join(strings.Fields(contract), " ")
	return strings.Split(flat, ". ")
}

// foldDocFabricationRE is a PRESENT-TENSE assertion that a shape emits a world
// no execution path produces. Past tense is deliberately unmatched: "the
// pre-#72 build fabricated" is a history, and a history is not a claim about
// this tree.
var foldDocFabricationRE = regexp.MustCompile(`(?i)\bfabricates\b|\bfalse positive\b`)

// foldDocNotCalledRE is the three-case justification #337 quoted: the reason the
// enclosing walk keeps folding around a closure. It is true of the three it
// names and silent about the fourth, which is exactly the sentence the issue
// says told an adopter this case cannot happen.
var foldDocNotCalledRE = regexp.MustCompile(`(?i)conditionally called, never called, or created after`)

// The contract must name the spelling that fabricates, and must not name one
// that does not.
func TestFoldContractNamesTheCallSpellingThatFabricates(t *testing.T) {
	sentences := foldDocSentences(foldDocContract(t))
	if len(sentences) < 20 {
		t.Fatalf("the contract block split into %d sentence(s); it is dozens of "+
			"lines of prose, so a split that small is matching something other than "+
			"sentences and would find no claim whatever the block says", len(sentences))
	}
	for _, call := range []string{foldDocBareCall, foldDocEscapeCall} {
		t.Run(call, func(t *testing.T) {
			measured := foldDocFabricates(t, call)
			claim := ""
			for _, s := range sentences {
				if strings.Contains(s, call) && foldDocFabricationRE.MatchString(s) {
					claim = s
					break
				}
			}
			switch {
			case measured && claim == "":
				t.Errorf("`%s` leaves the fold emitting a world built on the seed without "+
					"the closure's append — a value no execution path produces, on a query "+
					"every real path orders — and no sentence of fold.go's contract block "+
					"says so. That residue is disclosed in locking.go, the fold spec and "+
					"docs/reference.md; the block those three are downstream of is where a "+
					"reader of this pass looks first (#337). A sentence naming `%s` and "+
					"matching %s is what closes it.", call, call, foldDocFabricationRE)
			case !measured && claim != "":
				t.Errorf("fold.go's contract block calls `%s` a fabrication or a false "+
					"positive, and it is neither: invoked.go untracks q for that spelling "+
					"and the fold emits no world built on the seed at all (#72/#337). A "+
					"reader of `%s` is told to expect a finding that cannot occur, and a "+
					"reader holding the `%s` finding is pointed at the wrong shape. "+
					"Sentence: %q", call, call, foldDocEscapeCall, claim)
			}
		})
	}
}

// foldDocParagraphs splits the block on its blank comment lines and flattens
// each one, because every claim in it is wrapped across several lines and a
// pattern matched against the raw text would miss any phrase a wrap fell into.
func foldDocParagraphs(contract string) []string {
	var out []string
	for _, para := range strings.Split(contract, "\n\n") {
		out = append(out, strings.Join(strings.Fields(para), " "))
	}
	return out
}

// The three-case justification must carry its exception.
func TestFoldContractQualifiesTheNotCalledJustification(t *testing.T) {
	paras := foldDocParagraphs(foldDocContract(t))
	measured := foldDocFabricates(t, foldDocEscapeCall)

	found := 0
	for i, para := range paras {
		if !foldDocNotCalledRE.MatchString(para) {
			continue
		}
		found++
		names := strings.Contains(para, foldDocEscapeCall)
		switch {
		case measured && !names:
			t.Errorf("contract paragraph %d justifies emitting the outside-appends world "+
				"because the closure is %s the value is used, and does not name `%s` — "+
				"the spelling that is none of those three, runs on every real path, and "+
				"still leaves that world emitted. This is the sentence #337 quoted as the "+
				"reason the residue is undisclosed. Paragraph: %q",
				i, foldDocNotCalledRE, foldDocEscapeCall, para)
		case !measured && names:
			t.Errorf("contract paragraph %d still names `%s` as the exception to the "+
				"not-called justification, and the fold no longer emits a world built on "+
				"the seed for it — so the spelling is untracked now and this paragraph "+
				"tells a reader a residue reproduces that does not. A disclosure of a "+
				"closed gap misdirects triage exactly as a missing one does (#311, #313). "+
				"Paragraph: %q", i, foldDocEscapeCall, para)
		}
	}
	if found == 0 {
		t.Fatalf("no paragraph of fold.go's contract block matches %s: that justification "+
			"is why the enclosing walk keeps folding around a closure at all, and a run "+
			"that cannot find it is matching prose rewritten out from under this test "+
			"rather than checking it", foldDocNotCalledRE)
	}
}
