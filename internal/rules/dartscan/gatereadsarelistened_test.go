package dartscan

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/preprocess"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// dart/gate-reads-are-listened asserts that a rebuild-scoped gate can SEE every
// field it gates on.
//
// The defect class: an AnimatedBuilder/ListenableBuilder rebuilds a widget when
// one of the listenables it is given changes, and its builder computes an
// enable/disable decision from a set of controllers. When the decision reads a
// controller that is NOT in the merge list, the widget never rebuilds on that
// field, so the gate is computed from a stale build and silently does not gate.
//
// It has now recurred twice on the same card (once directly, once again
// through a seam adoption), and both times the omission was invisible: the
// getter and the merge list are far apart, the read is one indirection deep
// through a getter, and the code carried a comment asserting the gate
// reflected live input. That is the #8677 shape — an "every X routes through
// Y" invariant over source that no lexical pattern converges on.
func newGateReadsChecker(t *testing.T, params string) rules.Checker {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(params), &node); err != nil {
		t.Fatalf("bad params yaml: %v", err)
	}
	c, err := newGateReadsAreListened(node.Content[0])
	if err != nil {
		t.Fatalf("newGateReadsAreListened: %v", err)
	}
	return c
}

const gateReadsParams = `
builders: ['AnimatedBuilder', 'ListenableBuilder']
listen_args: ['animation', 'listenable']
builder_arg: builder
read_suffixes: ['.text']
`

func checkGateReads(t *testing.T, src string) []rules.Match {
	t.Helper()
	c := newGateReadsChecker(t, gateReadsParams)
	f := scan.NewMemFile("packages/feature_foo/lib/presentation/x.dart", preprocess.CodeOnlyDart([]byte(src)))
	got, err := c.CheckFile(f)
	if err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
	return got
}

// The live shape, reduced: the gate getter reads three controllers and the
// merge list carries two.
const panelCardOmitted = `
class _S extends State<W> {
  bool get _complete =>
      _hasProfile &&
      railSpacingRule.isValid(_railSpacing.text) &&
      braceSpacingRule.isValid(_brace.text) &&
      heightRule.isValid(_height.text);

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: Listenable.merge(<Listenable>[_railSpacing, _brace]),
      builder: (BuildContext context, _) => AsyncFilledButton(
        onPressed: _complete ? _save : null,
        child: const Text('Save'),
      ),
    );
  }
}
`

func TestGateReadsAreListened_OmittedControllerFires(t *testing.T) {
	got := checkGateReads(t, panelCardOmitted)
	if len(got) != 1 {
		t.Fatalf("a gate reading _height with _height absent from the merge list was accepted; want 1 match, got %d (%+v)", len(got), got)
	}
	if !contains(got[0].Message, "_height") {
		t.Errorf("finding does not name the unlistened controller: %q", got[0].Message)
	}
}

// The control: the same widget with the controller present. Proves the fire
// keys on the omission and not on the AnimatedBuilder shape.
func TestGateReadsAreListened_CompleteMergeListPasses(t *testing.T) {
	src := `
class _S extends State<W> {
  bool get _complete =>
      _hasProfile &&
      railSpacingRule.isValid(_railSpacing.text) &&
      braceSpacingRule.isValid(_brace.text) &&
      heightRule.isValid(_height.text);

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: Listenable.merge(<Listenable>[_railSpacing, _brace, _height]),
      builder: (BuildContext context, _) => AsyncFilledButton(
        onPressed: _complete ? _save : null,
        child: const Text('Save'),
      ),
    );
  }
}
`
	if got := checkGateReads(t, src); len(got) != 0 {
		t.Fatalf("complete merge list flagged: %+v", got)
	}
}

// A read written INLINE in the builder, with no getter indirection, is the same
// defect and must fire — otherwise the rule only sees the shape it was written
// against.
func TestGateReadsAreListened_InlineReadFires(t *testing.T) {
	src := `
class _S extends State<W> {
  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: Listenable.merge(<Listenable>[_a]),
      builder: (BuildContext context, _) => Button(
        onPressed: _a.text.isNotEmpty && _b.text.isNotEmpty ? _save : null,
      ),
    );
  }
}
`
	got := checkGateReads(t, src)
	if len(got) != 1 {
		t.Fatalf("inline read of an unlistened controller accepted; want 1, got %d (%+v)", len(got), got)
	}
	if !contains(got[0].Message, "_b") {
		t.Errorf("finding does not name _b: %q", got[0].Message)
	}
}

// A single listenable passed without Listenable.merge is still a listen set.
func TestGateReadsAreListened_SingleListenablePasses(t *testing.T) {
	src := `
class _S extends State<W> {
  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: _only,
      builder: (BuildContext context, _) => Text(_only.text),
    );
  }
}
`
	if got := checkGateReads(t, src); len(got) != 0 {
		t.Fatalf("single listenable form flagged: %+v", got)
	}
}

// A controller read OUTSIDE any builder is not a rebuild-scoped gate and is
// none of this rule's business.
func TestGateReadsAreListened_ReadOutsideBuilderIgnored(t *testing.T) {
	src := `
class _S extends State<W> {
  void _save() {
    final v = _elsewhere.text;
    send(v);
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: Listenable.merge(<Listenable>[_a]),
      builder: (BuildContext context, _) => Text(_a.text),
    );
  }
}
`
	if got := checkGateReads(t, src); len(got) != 0 {
		t.Fatalf("a read outside the builder was flagged: %+v", got)
	}
}

// A getter the builder does not call contributes no reads, so an unrelated
// getter elsewhere in the file cannot make an honest widget fire.
func TestGateReadsAreListened_UncalledGetterIgnored(t *testing.T) {
	src := `
class _S extends State<W> {
  bool get _unused => other.isValid(_far.text);

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: Listenable.merge(<Listenable>[_a]),
      builder: (BuildContext context, _) => Text(_a.text),
    );
  }
}
`
	if got := checkGateReads(t, src); len(got) != 0 {
		t.Fatalf("an uncalled getter's reads were attributed to the builder: %+v", got)
	}
}

// A block-bodied getter is the same declaration with different syntax.
func TestGateReadsAreListened_BlockBodiedGetterFires(t *testing.T) {
	src := `
class _S extends State<W> {
  bool get _complete {
    if (!rule.isValid(_height.text)) return false;
    return true;
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: Listenable.merge(<Listenable>[_a]),
      builder: (BuildContext context, _) => Button(onPressed: _complete ? _save : null),
    );
  }
}
`
	got := checkGateReads(t, src)
	if len(got) != 1 {
		t.Fatalf("block-bodied getter's read not attributed; want 1, got %d (%+v)", len(got), got)
	}
}

func TestGateReadsAreListened_Registered(t *testing.T) {
	if _, ok := rules.Lookup("dart/gate-reads-are-listened"); !ok {
		t.Fatalf("dart/gate-reads-are-listened is not registered; its rule cannot load")
	}
}

func contains(hay, needle string) bool {
	return len(hay) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(hay); i++ {
			if hay[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
