package dartscan_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
)

const dartDeclaredOnce = "dart/declared-once"

func dartOnce(t *testing.T, decls ...string) rules.Checker {
	t.Helper()
	var b strings.Builder
	b.WriteString("declarations:\n")
	for i := 0; i+1 < len(decls); i += 2 {
		b.WriteString("  - path: " + decls[i] + "\n    decl: " + decls[i+1] + "\n")
	}
	return mustChecker(t, dartDeclaredOnce, b.String())
}

func dartRunAll(t *testing.T, c rules.Checker, files map[string]string) []rules.Match {
	t.Helper()
	var out []rules.Match
	for path, src := range files {
		ms, err := c.CheckFile(file(path, src))
		if err != nil {
			t.Fatalf("CheckFile(%s): %v", path, err)
		}
		out = append(out, ms...)
	}
	return append(out, c.(rules.Finalizer).Finalize()...)
}

const appCard = `import 'package:flutter/widgets.dart';

class AppCard extends StatelessWidget {
  const AppCard({super.key});
}
`

func TestDartDeclaredOnceAloneIsClean(t *testing.T) {
	if got := dartRunAll(t, dartOnce(t, "lib/a.dart", "type AppCard"), map[string]string{"lib/a.dart": appCard}); len(got) != 0 {
		t.Fatalf("one listed declaration in its owner file is clean: %+v", got)
	}
}

func TestDartSecondDeclarationFails(t *testing.T) {
	got := dartRunAll(t, dartOnce(t, "lib/a.dart", "type AppCard"), map[string]string{
		"lib/a.dart": appCard,
		"lib/b.dart": "class AppCard extends StatelessWidget {}\n",
	})
	if len(got) != 1 || got[0].Path != "lib/b.dart" || got[0].Line != 1 {
		t.Fatalf("the second declaration is the finding: %+v", got)
	}
	if !strings.Contains(got[0].Message, "type AppCard is declared once, at lib/a.dart:3") {
		t.Fatalf("the finding names the owner: %q", got[0].Message)
	}
}

func TestDartAnnotatedDeclarationIsCounted(t *testing.T) {
	src := "@riverpod\nFuture<List<String>> colourOptions(Ref ref) async => [];\n"
	got := dartRunAll(t, dartOnce(t, "lib/a.dart", "function colourOptions"), map[string]string{
		"lib/a.dart": src,
		"lib/b.dart": "Future<List<String>> colourOptions(Ref ref) async => [];\n",
	})
	if len(got) != 1 || got[0].Path != "lib/b.dart" {
		t.Fatalf("a declaration under an annotation is still the owner: %+v", got)
	}
}

func TestDartEveryTopLevelKind(t *testing.T) {
	owner := "mixin Fades {}\nenum Tone { a }\nextension Figures on TextStyle {}\ntypedef Mapper = int Function(int);\n" +
		"const kDefaultMode = 'basic';\nString get label => 'x';\n"
	dup := "mixin Fades {}\nenum Tone { b }\nextension Figures on TextStyle {}\ntypedef Mapper = int Function(int);\nconst kDefaultMode = 'x';\nString get label => 'y';\n"
	c := dartOnce(t, "lib/a.dart", "type Fades", "lib/a.dart", "type Tone", "lib/a.dart", "type Figures",
		"lib/a.dart", "type Mapper", "lib/a.dart", "variable kDefaultMode", "lib/a.dart", "getter label")
	if got := dartRunAll(t, c, map[string]string{"lib/a.dart": owner, "lib/b.dart": dup}); len(got) != 6 {
		t.Fatalf("every top-level kind is held to one declaration: %+v", got)
	}
}

func TestDartMembersAreNotTopLevel(t *testing.T) {
	owner := "void applyOptimistic() {}\n"
	other := "class Store {\n  void applyOptimistic() {}\n  static const kDefaultMode = 'x';\n}\n"
	if got := dartRunAll(t, dartOnce(t, "lib/a.dart", "function applyOptimistic"), map[string]string{"lib/a.dart": owner, "lib/b.dart": other}); len(got) != 0 {
		t.Fatalf("a member named like a listed top-level declaration is not a second declaration: %+v", got)
	}
}

func TestDartPrivateNamesBelongToTheirLibrary(t *testing.T) {
	owner := "class _Body {}\n"
	c := func() rules.Checker { return dartOnce(t, "lib/a.dart", "type _Body") }
	if got := dartRunAll(t, c(), map[string]string{"lib/a.dart": owner, "lib/b.dart": "class _Body {}\n"}); len(got) != 0 {
		t.Fatalf("a private name in another library is another declaration: %+v", got)
	}
	if got := dartRunAll(t, c(), map[string]string{"lib/a.dart": owner + "class _Body {}\n"}); len(got) != 1 {
		t.Fatalf("a private name twice in its own library duplicates it: %+v", got)
	}
}

func TestDartMultilineStringsAndCommentsAreNotDeclarations(t *testing.T) {
	other := "const doc = '''\nclass AppCard extends StatelessWidget {}\n''';\n/*\nclass AppCard {}\n*/\n"
	if got := dartRunAll(t, dartOnce(t, "lib/a.dart", "type AppCard"), map[string]string{"lib/a.dart": appCard, "lib/b.dart": other}); len(got) != 0 {
		t.Fatalf("text inside a string or block comment declares nothing: %+v", got)
	}
}

func TestDartListedDeclarationMissingFromItsFileFails(t *testing.T) {
	got := dartRunAll(t, dartOnce(t, "lib/a.dart", "type AppPanel"), map[string]string{"lib/a.dart": appCard})
	if len(got) != 1 || got[0].Path != "lib/a.dart" || !strings.Contains(got[0].Message, "declares no top-level type AppPanel") {
		t.Fatalf("a listed declaration its file does not declare is stale, and says so: %+v", got)
	}
}

func TestDartDeclaredOnceConfig(t *testing.T) {
	factory, _ := rules.Lookup(dartDeclaredOnce)
	if factory == nil {
		t.Fatalf("type %q not registered", dartDeclaredOnce)
	}
	for _, p := range []string{
		"",
		"declarations: []",
		"declarations:\n  - path: ' '\n    decl: type AppCard",
		"declarations:\n  - path: a.dart\n    decl: class AppCard",
		"declarations:\n  - path: a.dart\n    decl: AppCard",
		"declarations:\n  - path: a.dart\n    decl: type AppCard\n  - path: b.dart\n    decl: type AppCard",
	} {
		if _, err := factory(paramsNode(t, p)); err == nil {
			t.Errorf("params %q must be a config error", p)
		}
	}
}

func TestDartDeclaredOnceIsAScopeBoundInvariant(t *testing.T) {
	c := dartOnce(t, "lib/a.dart", "type AppCard")
	if w, ok := c.(rules.WholeTreeInvariant); !ok || !w.WholeTreeInvariant() {
		t.Fatal("it counts across files, so it judges the whole tree")
	}
	if !rules.IsScopeBound(c) {
		t.Fatal("it reads only the files its scope admits")
	}
}
