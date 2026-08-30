// builder_internal_test.go — #311, what counts as a builder and what counts as
// text going into one.
//
// The corpus test in internal/rules/sqlparse measures one builder fixture, so it
// answers "does a builder produce a site" and nothing about which values are
// builders or which calls are writes. A mutation sweep left six narrowings alive
// there, and every one of them is a way for this file to be wrong in the
// direction that matters: recognising too much puts a census line against every
// `.WriteString` on every type in the repo, which is the flood that makes a
// diagnostic channel unreadable, and recognising too little restores the
// pre-#311 silence for the shape the block has disclosed longest.
//
// FIXTURES ARE PARSED AND NEVER TYPE-CHECKED, which is this package's stated
// design (spec §2: "the extractor is deliberately parse-only so it works on a
// tree that does not compile"). One fixture below therefore calls a real method
// with an argument its real signature would refuse: the branch under test reads
// the method NAME, and that is all the pass can see.
package sqlextract

import "testing"

const builderQuery = "SELECT id FROM t WHERE s = 'x'"

func builderSrc(params, body string) string {
	return "package db\n\nimport \"strings\"\n\nfunc load(" + params + ") string {\n" +
		body + "}\n"
}

// Every spelling that binds a builder is read, because what makes a name a
// builder is its TYPE, and Go writes that type in four different places. A
// spelling this misses is a query reported as clean with nothing said about it.
func TestEverySpellingThatBindsABuilderIsRead(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"var-with-type", builderSrc("",
			"\tvar sb strings.Builder\n\tsb.WriteString(\""+builderQuery+"\")\n\treturn sb.String()\n")},
		{"var-with-value", builderSrc("",
			"\tvar sb = strings.Builder{}\n\tsb.WriteString(\""+builderQuery+"\")\n\treturn sb.String()\n")},
		{"short-decl-literal", builderSrc("",
			"\tsb := strings.Builder{}\n\tsb.WriteString(\""+builderQuery+"\")\n\treturn sb.String()\n")},
		{"short-decl-address", builderSrc("",
			"\tsb := &strings.Builder{}\n\tsb.WriteString(\""+builderQuery+"\")\n\treturn sb.String()\n")},
		{"short-decl-new", builderSrc("",
			"\tsb := new(strings.Builder)\n\tsb.WriteString(\""+builderQuery+"\")\n\treturn sb.String()\n")},
		// The signature is the only place a helper's builder carries its type,
		// and a query split across functions is the ordinary reason to pass one.
		{"pointer-parameter", builderSrc("sb *strings.Builder",
			"\tsb.WriteString(\""+builderQuery+"\")\n\treturn sb.String()\n")},
		// bytes.Buffer is the same shape to this analysis and to a reader.
		{"bytes-buffer", "package db\n\nimport \"bytes\"\n\nfunc load() string {\n" +
			"\tvar buf bytes.Buffer\n\tbuf.WriteString(\"" + builderQuery + "\")\n" +
			"\treturn buf.String()\n}\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sitesOf(t, c.src)
			if len(got) != 1 || got[0].Key != reasonStringsBuilder.Key {
				t.Fatalf("this binds a builder and composes a query into it, so the "+
					"fold's silence about it has to be countable: %+v", got)
			}
		})
	}
}

// And the narrowings, each of which a mutation survived. A name that is not a
// builder, a call that is not a write, and a write carrying no literal text are
// three different ways to turn the census into a list of every method call in
// the repo.
func TestWhatIsNotABuilderCompositionIsNotReported(t *testing.T) {
	cases := []struct {
		name string
		src  string
		why  string
	}{
		{
			"not-a-builder-type",
			"package db\n\ntype log struct{}\n\nfunc load(l log) string {\n" +
				"\tl.WriteString(\"" + builderQuery + "\")\n\treturn \"\"\n}\n",
			"any type can have a WriteString; the census must not claim the SQL " +
				"gate declined every one of them",
		},
		{
			// Grow is a real strings.Builder method and takes an int; the pass
			// never type-checks, so the branch under test is the method NAME.
			"not-a-write-method",
			builderSrc("",
				"\tvar sb strings.Builder\n\tsb.Grow(\""+builderQuery+"\")\n\treturn sb.String()\n"),
			"only the calls that put text IN compose a query",
		},
		{
			"no-literal-text-written",
			builderSrc("a, b string",
				"\tvar sb strings.Builder\n\tsb.WriteString(a)\n\tsb.WriteString(b)\n\treturn sb.String()\n"),
			"a builder assembled from runtime values gives this pass no evidence " +
				"it holds SQL at all",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sitesOf(t, c.src); len(got) != 0 {
				t.Fatalf("%s: %+v", c.why, got)
			}
		})
	}
}

// One builder, one line, anchored at the first write. A five-call composition is
// one query; five census lines about it would rank a verbose builder above a
// hazardous one, and the count the whole channel exists to produce would be a
// count of WriteStrings.
func TestOneBuilderIsOneSiteAtItsFirstWrite(t *testing.T) {
	src := "package db\n\nimport \"strings\"\n\nfunc load() string {\n" +
		"\tvar sb strings.Builder\n" + // 6
		"\tsb.WriteString(\"SELECT id\")\n" + // 7
		"\tsb.WriteString(\" FROM t WHERE s = 'x'\")\n" + // 8
		"\tsb.WriteString(\" FOR UPDATE\")\n" + // 9
		"\treturn sb.String()\n}\n"
	got := sitesOf(t, src)
	if len(got) != 1 {
		t.Fatalf("one builder is one unreadable query: %+v", got)
	}
	if got[0].Line != 7 {
		t.Fatalf("anchored at line %d, want 7 — the first write is where the query "+
			"starts; the declaration above it says only that there is a builder",
			got[0].Line)
	}
	// The text is the accumulated literal, which is what lets a caller tell a
	// SQL-bearing builder from one assembling a log line. It is never rendered.
	if got[0].Text != "SELECT id FROM t WHERE s = 'x' FOR UPDATE" {
		t.Fatalf("text %q — a caller filtering for SQL judges this, so a builder "+
			"whose keyword and structure arrive in separate writes has to be "+
			"reassembled before the filter sees it", got[0].Text)
	}
}

// A builder is one query however many scopes its writes are spread across, and
// the two directions of that are different failures.
//
// WRITTEN INSIDE A CLOSURE is a MISS, and it is the one this walk had until the
// mutation sweep: the closure's own fold scope never sees the declaration, so a
// walk that stopped at *ast.FuncLit — as every other analysis in this package
// does — left the query invisible to both scopes and reported nowhere. That is
// the pre-#311 silence, rebuilt inside #311's own fix.
//
// DECLARED IN TWO NESTED SCOPES is the double-report the stop was guarding
// against, and the sink's dedup already answers it: one line, one key, one text.
func TestABuilderIsOneSiteAcrossNestedScopes(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"written-inside-a-closure", "package db\n\nimport \"strings\"\n\n" +
			"func load() string {\n\tvar sb strings.Builder\n" +
			"\tf := func() { sb.WriteString(\"" + builderQuery + "\") }\n" +
			"\tf()\n\treturn sb.String()\n}\n"},
		{"declared-inside-a-closure", "package db\n\nimport \"strings\"\n\n" +
			"func load() func() string {\n\treturn func() string {\n" +
			"\t\tvar sb strings.Builder\n" +
			"\t\tsb.WriteString(\"" + builderQuery + "\")\n" +
			"\t\treturn sb.String()\n\t}\n}\n"},
		{"shadowed-in-both-scopes", "package db\n\nimport \"strings\"\n\n" +
			"func load() string {\n\tvar sb strings.Builder\n" +
			"\t_ = func() string {\n\t\tvar sb strings.Builder\n" +
			"\t\tsb.WriteString(\"" + builderQuery + "\")\n" +
			"\t\treturn sb.String()\n\t}\n\treturn sb.String()\n}\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sitesOf(t, c.src); len(got) != 1 {
				t.Fatalf("one builder composing one query is one census line, "+
					"whichever scope walks it: %+v", got)
			}
		})
	}
}

// The pre-parse gate, and the reason it names the bare type and not the
// qualified spelling.
//
// FromGoReassembled runs on every `check` over every .go file a locking rule
// scopes, so the builder walk added for `lint`'s benefit is paid by the gate
// itself; skipping a file that cannot hold a builder is worth the five lines.
// The gate is only worth anything while it is SOUND, and the tight version is
// not: a selector may break after its dot, so this parses, isBuilderType matches
// it, and a substring search for "strings.Builder" does not. The identifier
// cannot be split, which is why the gate reads that instead.
func TestABuilderWhoseSelectorBreaksAfterTheDotIsStillRead(t *testing.T) {
	src := "package db\n\nimport \"strings\"\n\nfunc load() string {\n" +
		"\tvar sb strings.\n\t\tBuilder\n" +
		"\tsb.WriteString(\"" + builderQuery + "\")\n\treturn sb.String()\n}\n"
	got := sitesOf(t, src)
	if len(got) != 1 || got[0].Key != reasonStringsBuilder.Key {
		t.Fatalf("a gate that misses a spelling the recogniser accepts restores "+
			"the exact silence #311 closed: %+v", got)
	}
}
