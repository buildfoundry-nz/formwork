package dartscan_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
)

const dartSingleHome = "dart/marked-single-home"

func dartHome(t *testing.T) rules.Checker {
	t.Helper()
	return mustChecker(t, dartSingleHome, "marker: 'formwork:single-home'")
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

/// The one card every admin screen frames its content in.
// formwork:single-home
class AppCard extends StatelessWidget {
  const AppCard({super.key});
}
`

func TestDartSingleHomeAloneIsClean(t *testing.T) {
	if got := dartRunAll(t, dartHome(t), map[string]string{"lib/a.dart": appCard}); len(got) != 0 {
		t.Fatalf("one marked declaration is its own home: %+v", got)
	}
}

func TestDartSecondDeclarationFails(t *testing.T) {
	got := dartRunAll(t, dartHome(t), map[string]string{
		"lib/a.dart": appCard,
		"lib/b.dart": "class AppCard extends StatelessWidget {}\n",
	})
	if len(got) != 1 || got[0].Path != "lib/b.dart" || got[0].Line != 1 {
		t.Fatalf("the unmarked second declaration is the finding: %+v", got)
	}
	if !strings.Contains(got[0].Message, "lib/a.dart:5") {
		t.Fatalf("the finding names the home: %q", got[0].Message)
	}
}

func TestDartMarkerReachesPastAnnotationsAndDocs(t *testing.T) {
	src := "// formwork:single-home\n/// Options for the building-type picker.\n@riverpod\nFuture<List<String>> buildingTypeOptions(Ref ref) async => [];\n"
	got := dartRunAll(t, dartHome(t), map[string]string{
		"lib/a.dart": src,
		"lib/b.dart": "Future<List<String>> buildingTypeOptions(Ref ref) async => [];\n",
	})
	if len(got) != 1 || got[0].Path != "lib/b.dart" {
		t.Fatalf("a marker above the doc and annotation still marks the declaration: %+v", got)
	}
}

func TestDartEveryTopLevelKind(t *testing.T) {
	home := "// formwork:single-home\nmixin Fades {}\n// formwork:single-home\nenum Tone { a }\n" +
		"// formwork:single-home\nextension Figures on TextStyle {}\n// formwork:single-home\ntypedef Mapper = int Function(int);\n" +
		"// formwork:single-home\nconst kTrialShellOrgType = 'trial';\n// formwork:single-home\nString get label => 'x';\n"
	fork := "mixin Fades {}\nenum Tone { b }\nextension Figures on TextStyle {}\ntypedef Mapper = int Function(int);\nconst kTrialShellOrgType = 'x';\nString get label => 'y';\n"
	if got := dartRunAll(t, dartHome(t), map[string]string{"lib/a.dart": home, "lib/b.dart": fork}); len(got) != 6 {
		t.Fatalf("every top-level kind holds its home: %+v", got)
	}
}

func TestDartMembersAreNotTopLevel(t *testing.T) {
	home := "// formwork:single-home\nvoid applyOptimistic() {}\n"
	other := "class Store {\n  void applyOptimistic() {}\n  static const kTrialShellOrgType = 'x';\n}\n"
	if got := dartRunAll(t, dartHome(t), map[string]string{"lib/a.dart": home, "lib/b.dart": other}); len(got) != 0 {
		t.Fatalf("a member named like a marked top-level declaration is not a second declaration: %+v", got)
	}
}

func TestDartPrivateNamesBelongToTheirLibrary(t *testing.T) {
	home := "// formwork:single-home\nclass _Body {}\n"
	if got := dartRunAll(t, dartHome(t), map[string]string{"lib/a.dart": home, "lib/b.dart": "class _Body {}\n"}); len(got) != 0 {
		t.Fatalf("a private name in another library is another declaration: %+v", got)
	}
	if got := dartRunAll(t, dartHome(t), map[string]string{"lib/a.dart": home + "class _Body {}\n"}); len(got) != 1 {
		t.Fatalf("a private name twice in its own library forks it: %+v", got)
	}
}

func TestDartMultilineStringsAndCommentsAreNotDeclarations(t *testing.T) {
	other := "const doc = '''\nclass AppCard extends StatelessWidget {}\n''';\n/*\nclass AppCard {}\n*/\n"
	if got := dartRunAll(t, dartHome(t), map[string]string{"lib/a.dart": appCard, "lib/b.dart": other}); len(got) != 0 {
		t.Fatalf("text inside a string or block comment declares nothing: %+v", got)
	}
}

func TestDartTwoMarkedHomesBothFail(t *testing.T) {
	if got := dartRunAll(t, dartHome(t), map[string]string{"lib/a.dart": appCard, "lib/b.dart": appCard}); len(got) != 2 {
		t.Fatalf("two declarations both claiming the home are both findings: %+v", got)
	}
}

func TestDartStrayMarkerFails(t *testing.T) {
	src := "class Store {\n  // formwork:single-home\n  void apply() {}\n}\n"
	got := dartRunAll(t, dartHome(t), map[string]string{"lib/a.dart": src})
	if len(got) != 1 || got[0].Line != 2 {
		t.Fatalf("a marker on no top-level declaration holds nothing, and says so: %+v", got)
	}
}

func TestDartSingleHomeRequiresMarker(t *testing.T) {
	factory, _ := rules.Lookup(dartSingleHome)
	if factory == nil {
		t.Fatalf("type %q not registered", dartSingleHome)
	}
	for _, p := range []string{"", "marker: '  '"} {
		if _, err := factory(paramsNode(t, p)); err == nil {
			t.Fatalf("params %q must be a config error", p)
		}
	}
}

func TestDartSingleHomeIsAScopeBoundInvariant(t *testing.T) {
	c := dartHome(t)
	if w, ok := c.(rules.WholeTreeInvariant); !ok || !w.WholeTreeInvariant() {
		t.Fatal("it counts across files, so it judges the whole tree")
	}
	if !rules.IsScopeBound(c) {
		t.Fatal("it reads only the files its scope admits")
	}
}
