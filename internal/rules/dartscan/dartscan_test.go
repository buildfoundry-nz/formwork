package dartscan_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/dartscan"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

func paramsNode(t *testing.T, src string) *yaml.Node {
	t.Helper()
	if src == "" {
		return nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Content[0]
}

func mustChecker(t *testing.T, typeName, params string) rules.Checker {
	t.Helper()
	factory, ok := rules.Lookup(typeName)
	if !ok {
		t.Fatalf("type %q not registered", typeName)
	}
	c, err := factory(paramsNode(t, params))
	if err != nil {
		t.Fatalf("factory(%s): %v", typeName, err)
	}
	return c
}

func file(name, body string) *scan.File { return scan.NewMemFile(name, []byte(body)) }

func check(t *testing.T, c rules.Checker, f *scan.File) ([]rules.Match, error) {
	t.Helper()
	return c.CheckFile(f)
}

// ---- registration ----

func TestTypesRegistered(t *testing.T) {
	for _, n := range []string{"dart/ref-after-await", "dart/method-delegates"} {
		if _, ok := rules.Lookup(n); !ok {
			t.Errorf("type %q not registered", n)
		}
	}
}

// ---- param validation ----

func TestRefAfterAwaitRequiresMethod(t *testing.T) {
	factory, _ := rules.Lookup("dart/ref-after-await")
	if _, err := factory(paramsNode(t, "access: 'ref\\.read'\n")); err == nil {
		t.Fatal("missing method param must be an error")
	}
}

func TestRefAfterAwaitBadRegex(t *testing.T) {
	factory, _ := rules.Lookup("dart/ref-after-await")
	if _, err := factory(paramsNode(t, "method: '('\n")); err == nil {
		t.Fatal("invalid method regex must be an error")
	}
}

func TestRefAfterAwaitUnknownField(t *testing.T) {
	factory, _ := rules.Lookup("dart/ref-after-await")
	if _, err := factory(paramsNode(t, "method: 'build'\nbogus: 1\n")); err == nil {
		t.Fatal("unknown param field must be an error")
	}
}

func TestMethodDelegatesRequiresBoth(t *testing.T) {
	factory, _ := rules.Lookup("dart/method-delegates")
	if _, err := factory(paramsNode(t, "method: 'build'\n")); err == nil {
		t.Fatal("missing must_call must be an error")
	}
	if _, err := factory(paramsNode(t, "must_call: 'super'\n")); err == nil {
		t.Fatal("missing method must be an error")
	}
}

// ---- dart/ref-after-await ----

const refParams = "method: 'Widget build\\('\n"

func TestRefAfterAwaitFailsUnguarded(t *testing.T) {
	c := mustChecker(t, "dart/ref-after-await", refParams)
	src := `class X {
  Widget build(BuildContext context) async {
    await something();
    ref.read(provider);
  }
}
`
	ms, err := check(t, c, file("lib/x.dart", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("want 1 violation, got %+v", ms)
	}
	if ms[0].Line != 4 {
		t.Fatalf("want violation on line 4 (ref.read), got line %d", ms[0].Line)
	}
}

func TestRefAfterAwaitPassesGuardedSameLine(t *testing.T) {
	c := mustChecker(t, "dart/ref-after-await", refParams)
	src := `class X {
  Widget build(BuildContext context) async {
    await something();
    if (mounted) ref.read(provider);
  }
}
`
	ms, err := check(t, c, file("lib/x.dart", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("guarded same-line access should pass, got %+v", ms)
	}
}

func TestRefAfterAwaitPassesGuardedPrecedingLine(t *testing.T) {
	c := mustChecker(t, "dart/ref-after-await", refParams)
	src := `class X {
  Widget build(BuildContext context) async {
    await something();
    if (!mounted) return;
    ref.read(provider);
  }
}
`
	ms, err := check(t, c, file("lib/x.dart", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("access guarded by preceding if should pass, got %+v", ms)
	}
}

func TestRefAfterAwaitPassesAccessBeforeAwait(t *testing.T) {
	c := mustChecker(t, "dart/ref-after-await", refParams)
	src := `class X {
  Widget build(BuildContext context) async {
    ref.read(provider);
    await something();
  }
}
`
	ms, err := check(t, c, file("lib/x.dart", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("access before await should pass, got %+v", ms)
	}
}

func TestRefAfterAwaitIgnoresOutsideMethod(t *testing.T) {
	c := mustChecker(t, "dart/ref-after-await", refParams)
	// ref.read after await but NOT inside a build( method — not tracked.
	src := `class X {
  Widget other(BuildContext context) async {
    await something();
    ref.read(provider);
  }
}
`
	ms, err := check(t, c, file("lib/x.dart", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("access outside matching method should be ignored, got %+v", ms)
	}
}

func TestRefAfterAwaitSkipsNonDart(t *testing.T) {
	c := mustChecker(t, "dart/ref-after-await", refParams)
	src := `Widget build(BuildContext context) async {
  await something();
  ref.read(provider);
}
`
	ms, err := check(t, c, file("lib/x.go", src))
	if err != nil {
		t.Fatal(err)
	}
	if ms != nil {
		t.Fatalf("non-dart file must yield no findings, got %+v", ms)
	}
}

func TestRefAfterAwaitUnterminatedIsError(t *testing.T) {
	c := mustChecker(t, "dart/ref-after-await", refParams)
	// body opens but never closes -> heuristic parse failure -> error, not pass.
	src := `class X {
  Widget build(BuildContext context) async {
    await something();
`
	_, err := check(t, c, file("lib/x.dart", src))
	if err == nil {
		t.Fatal("unterminated method body must be a returned error, not a silent pass")
	}
}

func TestRefAfterAwaitDefaultAccessWatch(t *testing.T) {
	c := mustChecker(t, "dart/ref-after-await", refParams)
	src := `class X {
  Widget build(BuildContext context) async {
    await something();
    ref.watch(provider);
  }
}
`
	ms, err := check(t, c, file("lib/x.dart", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("default access should catch ref.watch too, got %+v", ms)
	}
}

// ---- dart/method-delegates ----

const delParams = "method: 'void dispose\\('\nmust_call: 'super\\.dispose\\('\n"

func TestMethodDelegatesPasses(t *testing.T) {
	c := mustChecker(t, "dart/method-delegates", delParams)
	src := `class X {
  void dispose() {
    controller.dispose();
    super.dispose();
  }
}
`
	ms, err := check(t, c, file("lib/x.dart", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("method that calls super.dispose should pass, got %+v", ms)
	}
}

func TestMethodDelegatesFails(t *testing.T) {
	c := mustChecker(t, "dart/method-delegates", delParams)
	src := `class X {
  void dispose() {
    controller.dispose();
  }
}
`
	ms, err := check(t, c, file("lib/x.dart", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("want 1 violation, got %+v", ms)
	}
	if ms[0].Line != 2 {
		t.Fatalf("violation must anchor to the header line (2), got %d", ms[0].Line)
	}
}

func TestMethodDelegatesOneLinerPass(t *testing.T) {
	c := mustChecker(t, "dart/method-delegates", delParams)
	src := `class X {
  void dispose() { super.dispose(); }
}
`
	ms, err := check(t, c, file("lib/x.dart", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("one-line method calling super.dispose should pass, got %+v", ms)
	}
}

func TestMethodDelegatesOneLinerFail(t *testing.T) {
	c := mustChecker(t, "dart/method-delegates", delParams)
	src := `class X {
  void dispose() { controller.dispose(); }
}
`
	ms, err := check(t, c, file("lib/x.dart", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].Line != 2 {
		t.Fatalf("one-line method missing super.dispose should fail at line 2, got %+v", ms)
	}
}

func TestMethodDelegatesSkipsAbstract(t *testing.T) {
	c := mustChecker(t, "dart/method-delegates", delParams)
	// abstract declaration has no body; heuristic skips it (no false positive).
	src := `abstract class X {
  void dispose();
}
`
	ms, err := check(t, c, file("lib/x.dart", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("abstract method declaration must be skipped, got %+v", ms)
	}
}

func TestMethodDelegatesSkipsNonDart(t *testing.T) {
	c := mustChecker(t, "dart/method-delegates", delParams)
	src := `void dispose() {
  controller.dispose();
}
`
	ms, err := check(t, c, file("lib/x.go", src))
	if err != nil {
		t.Fatal(err)
	}
	if ms != nil {
		t.Fatalf("non-dart file must yield no findings, got %+v", ms)
	}
}

func TestMethodDelegatesTwoMethods(t *testing.T) {
	c := mustChecker(t, "dart/method-delegates", delParams)
	src := `class X {
  void dispose() {
    super.dispose();
  }
  void dispose() {
    controller.dispose();
  }
}
`
	ms, err := check(t, c, file("lib/x.dart", src))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("want exactly the second method flagged, got %+v", ms)
	}
	if ms[0].Line != 5 {
		t.Fatalf("want violation at line 5, got %d", ms[0].Line)
	}
	if !strings.Contains(ms[0].Message, "super") {
		t.Fatalf("message should name the required call: %q", ms[0].Message)
	}
}

func TestMethodDelegatesUnterminatedIsError(t *testing.T) {
	c := mustChecker(t, "dart/method-delegates", delParams)
	src := `class X {
  void dispose() {
    controller.dispose();
`
	_, err := check(t, c, file("lib/x.dart", src))
	if err == nil {
		t.Fatal("unterminated method body must be a returned error")
	}
}
