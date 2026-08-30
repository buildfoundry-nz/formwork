// sites_internal_test.go — #311, the narrowings a corpus-level test cannot see.
//
// census_sites_test.go asserts the invariant an operator cares about: every
// composition the fold declines is reported, and nothing it reads is. That test
// is driven by the nineteen disclosed shapes, so it measures WHETHER a site
// appears and nothing about WHICH one — and a mutation sweep found nine
// narrowings it left alive: the dedup, the ordering, the empty-text refusal, and
// the "earliest write wins" rule in each of the four analyses that carry a
// position.
//
// Each of those is a claim about what an operator reads. A duplicated line, a
// permuting order, a refusal with no query behind it, or an anchor on the last
// write instead of the first are all ways for a diagnostic channel to be present
// and useless — which is the failure mode #75 shipped, one layer down.
//
// Inside the package because Site positions and text are what is under test, and
// sqlparse's looksLikeSQL filter would hide half of these behind a seed that
// happens not to look like SQL.
package sqlextract

import "testing"

// siteSrc wraps a body in `func load(params) string` seeded with a SQL literal,
// the same shape the fold tests use.
func siteSrc(params, body string) string {
	return "package db\n\nfunc load(" + params + ") string {\n" +
		"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" + body + "\treturn q\n}\n"
}

func sitesOf(t *testing.T, src string) []Site {
	t.Helper()
	_, sites, err := FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("FromGoReassembled: %v", err)
	}
	return sites
}

// One refusal, one line. A name re-seeded with the same text is refused twice at
// the same escape, and printing that twice tells an operator there are two
// unreadable queries where there is one — the census is a count, and a count
// that double-reports is worse than no count.
func TestOneConstructRefusingTwiceIsReportedOnce(t *testing.T) {
	src := siteSrc("",
		"\torderIt(&q)\n\tq = \"SELECT id FROM t WHERE s = 'x'\"\n")
	got := sitesOf(t, src)
	if len(got) != 1 {
		t.Fatalf("two refusals at one escape, of one query, must be one line: %+v", got)
	}
}

// Order is fixed, and it is not the order the walk happens to record in. A
// builder is collected at scope entry, before the statement walk, so its site is
// recorded FIRST however late in the function it sits — and an operator diffing
// two runs of the census on one file has to see the same lines in the same
// places.
func TestSitesAreOrderedBySourcePosition(t *testing.T) {
	src := "package db\n\nimport \"strings\"\n\nfunc load() string {\n" +
		"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" + // 6
		"\torderIt(&q)\n" + // 7: fold site
		"\tvar sb strings.Builder\n" + // 8
		"\tsb.WriteString(\"SELECT name FROM u WHERE a = 1\")\n" + // 9: builder site
		"\treturn q + sb.String()\n}\n"
	got := sitesOf(t, src)
	if len(got) != 2 {
		t.Fatalf("one escape and one builder, want two sites: %+v", got)
	}
	if got[0].Line >= got[1].Line {
		t.Fatalf("sites came back at lines %d then %d — the builder is collected "+
			"before the statement walk, so recording order is not source order and "+
			"the census would permute", got[0].Line, got[1].Line)
	}
}

// A refusal with no text behind it is not a query. It is the fold declining to
// track a name it has no evidence ever held SQL, and a consumer filtering for
// SQL cannot tell it from a declined import path — so it is dropped here rather
// than passed on for someone else to guess about.
func TestARefusalWithNoTextIsNotASite(t *testing.T) {
	src := "package db\n\nfunc load() string {\n\tq := \"\"\n\torderIt(&q)\n" +
		"\treturn q\n}\n"
	if got := sitesOf(t, src); len(got) != 0 {
		t.Fatalf("an empty seed is no evidence of a query: %+v", got)
	}
}

// The four analyses that carry a position each answer "where did this stop being
// readable", and each can see the name written more than once. The FIRST is the
// answer in all four: it is where the query stopped being ours, an operator
// reading downward finds it before the writes that follow, and — the reason it
// is pinned rather than left to taste — a map-iteration-order answer would move
// between runs.
func TestARefusalIsAnchoredAtTheFirstUnreadableWrite(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// escapedNames: two hand-outs of the same address.
			"address-escape",
			siteSrc("", "\torderIt(&q)\n\tlockIt(&q)\n"),
			5,
		},
		{
			// aliasUnsafe: two writes through the pointer.
			"deref-write",
			siteSrc("", "\tp := &q\n\t*p += \" ORDER BY id\"\n\t*p += \" FOR UPDATE\"\n"),
			6,
		},
		{
			// literalAppends: one called closure appending twice.
			"called-closure",
			siteSrc("", "\tadd := func() {\n\t\tq += \" ORDER BY id\"\n\t\tq += \" FOR UPDATE\"\n\t}\n\tadd()\n"),
			6,
		},
		{
			// recordUnreadAppends: a disqualified IIFE appending twice. Anchored
			// at the append and not at the literal, which is the whole point —
			// the literal's own line says only that there is a closure here.
			"disqualified-iife",
			siteSrc("", "\tfunc() {\n\t\tq += \" ORDER BY id\"\n\t\tnoop()\n\t\tq += \" FOR UPDATE\"\n\t}()\n"),
			6,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sitesOf(t, c.src)
			if len(got) != 1 {
				t.Fatalf("one refused variable, one site: %+v", got)
			}
			if got[0].Line != c.want {
				t.Fatalf("anchored at line %d, want %d — the first unreadable write "+
					"is the answer; anything else moves with map iteration order or "+
					"points past the place the query stopped being readable",
					got[0].Line, c.want)
			}
		})
	}
}

// TWO called closures appending to one variable is the case where the anchor is
// chosen by a MAP, and the failure it guards against is not a wrong line but a
// line that moves. Go randomises map iteration, so taking the last append seen
// gives a census that names line 5 on one run and line 6 on the next over
// unchanged source — a diagnostic channel nobody can diff, which is a slower
// version of having no channel at all.
//
// Repeated rather than asserted once, because that is what makes a
// nondeterministic answer a reliable failure instead of a flake.
func TestTwoClosuresWritingOneVariableAnchorDeterministically(t *testing.T) {
	src := siteSrc("",
		"\tadd := func() { q += \" FOR UPDATE\" }\n"+ // 5
			"\tmore := func() { q += \" ORDER BY id\" }\n"+ // 6
			"\tadd()\n\tmore()\n")
	for i := 0; i < 50; i++ {
		got := sitesOf(t, src)
		if len(got) != 1 {
			t.Fatalf("one refused variable, one site: %+v", got)
		}
		if got[0].Line != 5 {
			t.Fatalf("run %d anchored at line %d, want 5 — two closures append to "+
				"one query and the earliest write is the answer; anything else is "+
				"whichever the map handed over last", i, got[0].Line)
		}
	}
}

// WHAT "THIS CONSTRUCT APPENDS TO THE QUERY" MEANS, in the two places #311
// records a partial read. Getting it wrong invents a coverage gap that does not
// exist, which is the false claim #311 closed pointed the other way — and the
// first two cases below were live in the fix's own first draft.
//
// A closure REACHED AS A VALUE has not run. `pick(xs, func(){ q += … })` hands
// it to a call that may never invoke it, so the world without its append is a
// real execution path, the fold is right to emit it, and a line saying the query
// was read only in part is noise with a false reason attached — the reason names
// a closure "invoked", and this one is not.
//
// A closure the construct INVOKES has run, whatever the syntax, which is why one
// rule answers an `if` header's condition and its Init alike. Splitting those is
// the per-syntax-form patching that produced this class.
func TestOnlyAClosureThatProvablyRanIsAPartialRead(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		sites int
		key   string
	}{
		{
			"literal-passed-to-a-call-in-the-condition",
			siteSrc("xs []string",
				"\tif pick(xs, func() string { q += \" ORDER BY id\"; return q }) {\n\t}\n"),
			0, "",
		},
		{
			"literal-never-called-inside-a-disqualified-iife",
			siteSrc("",
				"\tfunc() {\n\t\tnoop()\n\t\tg := func() { q += \" ORDER BY id\" }\n\t\t_ = g\n\t}()\n"),
			0, "",
		},
		{
			"literal-invoked-in-the-condition",
			siteSrc("",
				"\tif func() bool { q += \" ORDER BY id\"; return true }() {\n\t}\n"),
			1, reasonHeaderLiteral.Key,
		},
		{
			// Valid Go, reached by neither iifeBody nor untrackAssigned, and
			// disclosed by nothing until the same rule covered both halves of
			// the header.
			"literal-invoked-in-the-init",
			siteSrc("b bool",
				"\tif func() { q += \" ORDER BY id\" }(); b {\n\t}\n"),
			1, reasonHeaderLiteral.Key,
		},
		{
			// A disqualified IIFE that invokes another literal: that body runs
			// too, so its append is text the fold did not read.
			"literal-invoked-inside-a-disqualified-iife",
			siteSrc("",
				"\tfunc() {\n\t\tnoop()\n\t\tfunc() { q += \" ORDER BY id\" }()\n\t}()\n"),
			1, reasonDisqualifiedIIFE.Key,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sitesOf(t, c.src)
			if len(got) != c.sites {
				t.Fatalf("want %d site(s), got %+v", c.sites, got)
			}
			if c.sites > 0 && got[0].Key != c.key {
				t.Fatalf("site key %q, want %q", got[0].Key, c.key)
			}
		})
	}
}
