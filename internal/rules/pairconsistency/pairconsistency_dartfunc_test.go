package pairconsistency_test

// where: same-func over DART, the grain the validating port needed. The
// unit is one function/method body, extracted with the same brace-depth
// heuristic the dart/* analyzers already use — no Go Dart parser exists, so
// braces inside string literals and comments are indistinguishable from
// code (see
// internal/rules/dartscan's package doc). Container declarations (class,
// enum, mixin, extension) open SCOPES, not units: a companion anywhere in a
// class must not greenwash a bare sibling method, which is exactly the
// file-grain count-blindness the same-func grain exists to close.
//
// Provenance: every firing/erroring test here was RED before the .dart
// dispatch existed (a .dart file in a same-func scope yielded no findings at
// all — the total blindness the issue names). The two no-finding pins
// (arrow member, map-literal field) were already green and say so; they pin
// the disclosed residue, mirroring the Go mode's package-level initializer
// exclusion.

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

const dartPairParams = "trigger: 'applyOptimistic'\nrequires: 'revertOptimistic'\nwhere: same-func\n"

func TestPairConsistencySameFuncDartBareMethodFires(t *testing.T) {
	src := `class AnnotationWriter {
  void write() {
    applyOptimistic(entry);
    persist(entry);
  }
}
`
	c := mustChecker(t, dartPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("annotation/writer.dart", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("bare method must fire once, got %+v", ms)
	}
	if ms[0].Line != 3 {
		t.Fatalf("finding must anchor on the trigger line (3), not the header, got line %d", ms[0].Line)
	}
	if !strings.Contains(ms[0].Message, "write") {
		t.Fatalf("message must name the offending method, got %q", ms[0].Message)
	}
}

// The hole same-file leaves open: one companion-bearing method must not buy
// the companion for a bare sibling method in the same class.
func TestPairConsistencySameFuncDartGreenwashSiblingDoesNotSatisfy(t *testing.T) {
	src := `class AnnotationWriter {
  void good() {
    applyOptimistic(entry);
    revertOptimistic(entry);
  }

  void bad() {
    applyOptimistic(entry);
    persist(entry);
  }
}
`
	c := mustChecker(t, dartPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("annotation/writer.dart", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("same-func must flag only the bare sibling, got %+v", ms)
	}
	if ms[0].Line != 8 {
		t.Fatalf("finding must anchor on the bare sibling's trigger line (8), got line %d", ms[0].Line)
	}
	if !strings.Contains(ms[0].Message, "bad") {
		t.Fatalf("finding must name the bare method bad, got %q", ms[0].Message)
	}
}

func TestPairConsistencySameFuncDartCompanionInSameMethodPasses(t *testing.T) {
	src := `class AnnotationWriter {
  void write() {
    applyOptimistic(entry);
    revertOptimistic(entry);
  }
}
`
	c := mustChecker(t, dartPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("annotation/writer.dart", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("companion in the same method must pass, got %+v", ms)
	}
}

// A multi-line signature must not blind the unit: the header accumulates
// until the body opens (the Dart analogue of the #9767 Go shape).
func TestPairConsistencySameFuncDartMultiLineSignatureFires(t *testing.T) {
	src := `class Loader {
  Future<void> load(
    String id,
  ) async {
    applyOptimistic(entry);
    persist(entry);
  }
}
`
	c := mustChecker(t, dartPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("loader.dart", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("multi-line signature bare body must fire once, got %+v", ms)
	}
	if ms[0].Line != 5 {
		t.Fatalf("finding must anchor on the trigger line (5), got line %d", ms[0].Line)
	}
	if !strings.Contains(ms[0].Message, "load") {
		t.Fatalf("message must name the method from its accumulated header, got %q", ms[0].Message)
	}
}

func TestPairConsistencySameFuncDartGetterIsAUnit(t *testing.T) {
	src := `class Copy {
  String get label {
    applyOptimistic(entry);
    return 'x';
  }
}
`
	c := mustChecker(t, dartPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("copy.dart", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || !strings.Contains(ms[0].Message, "label") {
		t.Fatalf("brace-bodied getter must be a unit named label, got %+v", ms)
	}
}

func TestPairConsistencySameFuncDartTopLevelFunctionFires(t *testing.T) {
	src := `void sweep() {
  applyOptimistic(entry);
  persist(entry);
}
`
	c := mustChecker(t, dartPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("sweep.dart", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || !strings.Contains(ms[0].Message, "sweep") {
		t.Fatalf("top-level function must be a unit named sweep, got %+v", ms)
	}
}

// A var-bound closure at declaration scope is executable code no method
// header owns — the Dart analogue of Go's var-bound func-literal unit.
func TestPairConsistencySameFuncDartFieldClosureIsAUnit(t *testing.T) {
	src := `class Handlers {
  final write = () {
    applyOptimistic(entry);
    persist(entry);
  };
}
`
	c := mustChecker(t, dartPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("handlers.dart", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || !strings.Contains(ms[0].Message, "write") {
		t.Fatalf("field-bound closure must be a unit named by its var, got %+v", ms)
	}
}

func TestPairConsistencySameFuncDartCountableObligation(t *testing.T) {
	src := `class Multi {
  void write() {
    applyOptimistic(a);
    applyOptimistic(b);
    revertOptimistic(a);
  }
}
`
	c := mustChecker(t, dartPairParams+"obligation: countable\n")
	ms, err := c.CheckFile(scan.NewMemFile("multi.dart", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || !strings.Contains(ms[0].Message, "countable") {
		t.Fatalf("2 triggers against 1 companion must fire the countable verdict, got %+v", ms)
	}
}

// Unbalanced braces at EOF inside an open unit are a parse failure (engine
// error, exit 2), never a silent skip — the dartscan contract.
func TestPairConsistencySameFuncDartUnterminatedBodyIsError(t *testing.T) {
	c := mustChecker(t, dartPairParams)
	_, err := c.CheckFile(scan.NewMemFile("broken.dart", []byte("class Broken {\n  void write() {\n    applyOptimistic(entry);\n")))
	if err == nil {
		t.Fatal("unterminated method body must be an error, not a silent skip")
	}
	if !strings.Contains(err.Error(), "broken.dart") {
		t.Fatalf("error must name the file, got %v", err)
	}
}

// (green from RED) Pin, not evidence about the change: an arrow-bodied
// member has no brace body, so it is not a unit — a trigger inside one is
// unjudged. Disclosed residue; use same-file where it matters.
func TestPairConsistencySameFuncDartArrowMemberHasNoUnit(t *testing.T) {
	c := mustChecker(t, dartPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("copy.dart", []byte("class Copy {\n  String label() => describe(applyOptimistic);\n}\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("arrow-bodied member must stay un-united, got %+v", ms)
	}
}

// (green from RED) Pin: a collection-literal field initializer opens a
// block, not a unit — `= {` is not a function header. Definition sites
// legitimately match a trigger; uniting them would false-fire the rule on
// its own vocabulary (mirrors the Go mode's initializer exclusion).
func TestPairConsistencySameFuncDartMapLiteralFieldHasNoUnit(t *testing.T) {
	src := `class Flags {
  final handlers = {
    'applyOptimistic': true,
  };
}
`
	c := mustChecker(t, dartPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("flags.dart", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("map-literal initializer must not become a unit, got %+v", ms)
	}
}

// A multi-line signature with NAMED parameters opens a `{` inside the
// parameter list and closes it on the line that opens the body — `String
// id, {` … `}) async {`. The walker took the named-parameter brace for a
// candidate body opener (resetting the accumulated signature) and then set
// the body's open depth before counting the `}` earlier on its own line, so
// the unit closed on the line it opened and the body was never a unit: every
// same-func rule was blind to every such function. Measured on a production
// corpus: a Dart same-func rule whose only trigger sat in a named-parameter
// function stayed green with its companion deleted.
func TestPairConsistencySameFuncDartNamedParamsMultiLineFires(t *testing.T) {
	src := `Future<int> foldPageSnapshot(
  Ref ref,
  String pageId, {
  required String failureEvent,
  bool foldCarriedView = true,
}) async {
  applyOptimistic(entry);
  return 1;
}
`
	c := mustChecker(t, dartPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("fold.dart", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("a named-parameter function's bare body must fire once, got %+v", ms)
	}
	if ms[0].Line != 7 {
		t.Fatalf("finding must anchor on the trigger line (7), got line %d", ms[0].Line)
	}
	if !strings.Contains(ms[0].Message, "foldPageSnapshot") {
		t.Fatalf("message must name the function from its full signature, got %q", ms[0].Message)
	}
}

// The single-line spelling — `void f({int a = 1}) {` — has the named-param
// braces before the body opener on one line. The first `{` is a parameter
// brace, not the body.
func TestPairConsistencySameFuncDartNamedParamsOneLineFires(t *testing.T) {
	src := `class Loader {
  void load({required String id, Map<String, int> seen = const {}}) {
    applyOptimistic(entry);
  }
}
`
	c := mustChecker(t, dartPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("loader.dart", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].Line != 3 || !strings.Contains(ms[0].Message, "load") {
		t.Fatalf("a one-line named-parameter method's bare body must fire once on line 3 naming load, got %+v", ms)
	}
}

// The companion inside a named-parameter function's body clears it, and the
// unit ends at the body's closing brace: a sibling after it is its own unit.
func TestPairConsistencySameFuncDartNamedParamsCompanionPassesAndUnitEnds(t *testing.T) {
	src := `class Loader {
  Future<void> load(
    String id, {
    bool force = false,
  }) async {
    applyOptimistic(entry);
    revertOptimistic(entry);
  }

  void other() {
    applyOptimistic(entry);
  }
}
`
	c := mustChecker(t, dartPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("loader.dart", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].Line != 11 || !strings.Contains(ms[0].Message, "other") {
		t.Fatalf("load passes; other is its own unit and fires on line 11, got %+v", ms)
	}
}

// An annotation with arguments on the line before a class — Riverpod's
// `@Riverpod(keepAlive: true)` — joined the accumulated header, and the
// container test did not allow `(…)` in a leading annotation, so the CLASS
// read as one function unit: every method in it shared one obligation and a
// companion in one method answered for a sibling's trigger.
func TestPairConsistencySameFuncDartAnnotatedClassIsAContainer(t *testing.T) {
	src := `@Riverpod(keepAlive: true)
class WallWidthController extends _$WallWidthController {
  Future<void> setWidth() async {
    applyOptimistic(entry);
  }

  Future<void> materialize() async {
    applyOptimistic(entry);
    revertOptimistic(entry);
  }
}
`
	c := mustChecker(t, dartPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("wall_width_controller.dart", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].Line != 4 || !strings.Contains(ms[0].Message, "setWidth") {
		t.Fatalf("the annotated class is a container; setWidth is its own unit and fires on line 4, got %+v", ms)
	}
}
