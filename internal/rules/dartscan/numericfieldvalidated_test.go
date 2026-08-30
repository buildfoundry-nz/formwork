package dartscan

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/preprocess"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// dart/numeric-field-validated asserts a STRUCTURAL invariant over an
// invocation's argument list: an invocation that declares a numeric
// `keyboardType` also supplies a live `validator`. The two arguments sit in the
// same list, arbitrarily far apart, interleaved with nested invocations that
// carry argument names of their own — so the checker reads DEPTH-1 arguments
// only. These tests pin that depth discipline, because it is the whole
// difference between this rule and the lexical shape that never converges
// (#8677).
//
// The fixtures under .formwork/fixtures/ prove the rule fires and passes
// end-to-end. These prove the argument walk underneath them, which a fixture
// reaching only the verdict cannot.

func newChecker(t *testing.T, params string) rules.Checker {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(params), &node); err != nil {
		t.Fatalf("bad params yaml: %v", err)
	}
	// yaml.Unmarshal wraps in a document node; the factory wants the mapping.
	c, err := newNumericFieldValidated(node.Content[0])
	if err != nil {
		t.Fatalf("newNumericFieldValidated: %v", err)
	}
	return c
}

const defaultParams = `
numeric_value: 'TextInputType\.(number|numberWithOptions)\b'
keyboard_arg: keyboardType
validator_arg: validator
validator_source: '^[A-Za-z_][A-Za-z0-9_]*(\((?:[^()]|\((?:[^()]|\([^()]*\))*\))*\))?(\??\.[A-Za-z_][A-Za-z0-9_]*(\((?:[^()]|\((?:[^()]|\([^()]*\))*\))*\))?)*$'
`

// checkSource runs the rule over src exactly as the engine does — through the
// code-only-dart projection the rule's YAML declares.
func checkSource(t *testing.T, src string) []rules.Match {
	t.Helper()
	c := newChecker(t, defaultParams)
	f := scan.NewMemFile("packages/feature_foo/lib/presentation/x.dart", preprocess.CodeOnlyDart([]byte(src)))
	got, err := c.CheckFile(f)
	if err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
	return got
}

func TestFiresOnNumericFieldWithoutValidator(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{
			name: "bare TextField, which has no validator parameter at all",
			src: `Widget b() => TextField(
  keyboardType: TextInputType.number,
  onChanged: emit,
);`,
		},
		{
			name: "nested numberWithOptions argument list must not read as this field's args",
			src: `Widget b() => TextField(
  keyboardType: const TextInputType.numberWithOptions(
    decimal: true,
    signed: true,
  ),
  onChanged: emit,
);`,
		},
		{
			name: "wrapper widget call site declares the numeric intent",
			src: `Widget b() => JobFormAiTextField(
  keyboardType: const TextInputType.numberWithOptions(decimal: true),
  onChanged: emit,
);`,
		},
		{
			name: "private widget counts",
			src: `Widget b() => _ThresholdInput(
  keyboardType: TextInputType.number,
);`,
		},
		{
			name: "generic invocation counts",
			src: `Widget b() => EditableFieldInput<BusinessRules, UpdateBusinessRulesRequest>(
  keyboardType: TextInputType.number,
);`,
		},
		{
			name: "an inert validator supplies the argument and validates nothing",
			src: `Widget b() => TextFormField(
  keyboardType: TextInputType.number,
  validator: (_) => null,
);`,
		},
		{
			name: "an explicitly null validator is inert too",
			src: `Widget b() => TextFormField(
  keyboardType: TextInputType.number,
  validator: null,
);`,
		},
		{
			// The regrowth path the seam exists to close: a fresh predicate
			// written at the field instead of a rule declared beside it. It
			// paints the field, so a has-a-validator rule would pass it, and
			// the write path is then free to disagree with it — which is the
			// whole defect class (#9925).
			name: "an open-coded inline predicate is not routing through the seam",
			src: `Widget b() => TextFormField(
  keyboardType: TextInputType.number,
  validator: (v) => double.tryParse(v ?? '') == null ? 'Bad' : null,
);`,
		},
		{
			name: "a block-bodied inline predicate is no better",
			src: `Widget b() => TextFormField(
  keyboardType: TextInputType.number,
  validator: (String? v) {
    return v == null ? 'Bad' : null;
  },
);`,
		},
		{
			name: "a nested field is found, not skipped with its parent",
			src: `Widget b() => Row(
  children: [
    TextFormField(
      keyboardType: TextInputType.number,
      validator: rule.validate,
    ),
    TextField(
      keyboardType: TextInputType.number,
    ),
  ],
);`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkSource(t, tc.src); len(got) != 1 {
				t.Fatalf("want exactly 1 finding, got %d: %+v", len(got), got)
			}
		})
	}
}

func TestPassesValidatedAndNonNumericFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{
			name: "validator as a tear-off",
			src: `Widget b() => TextFormField(
  keyboardType: const TextInputType.numberWithOptions(decimal: true),
  validator: kFloorArea.validate,
);`,
		},
		{
			// Every shape the tree actually binds today, surveyed off the rule
			// itself: a rule tear-off, a bare rule value (EditableField takes
			// the RULE, not a closure), the server mirror direct, a
			// null-aware hop, a parameterised rule factory, and a wrapper
			// forwarding its own validator parameter.
			name: "a rule tear-off",
			src: `Widget b() => TextFormField(
  keyboardType: TextInputType.number,
  validator: editableValueRule.validate,
);`,
		},
		{
			name: "a bare rule value",
			src: `Widget b() => EditableFieldInput(
  keyboardType: TextInputType.number,
  validator: _gstRule,
);`,
		},
		{
			name: "the server mirror direct",
			src: `Widget b() => TextFormField(
  keyboardType: TextInputType.number,
  validator: ServerValidationRules.validateOptionalFloorArea,
);`,
		},
		{
			name: "a null-aware hop through a nullable rule",
			src: `Widget b() => TextFormField(
  keyboardType: TextInputType.number,
  validator: field.validator?.validate,
);`,
		},
		{
			name: "a parameterised rule factory",
			src: `Widget b() => AppTextField(
  keyboardType: TextInputType.number,
  validator: framingInputRule(widget.input),
);`,
		},
		{
			name: "a wrapper forwarding its own validator parameter",
			src: `Widget b() => TextFormField(
  keyboardType: TextInputType.number,
  validator: validator,
);`,
		},
		{
			name: "non-numeric keyboard",
			src: `Widget b() => TextFormField(
  keyboardType: TextInputType.emailAddress,
);`,
		},
		{
			name: "phone is server-validated free text, not a parsed number",
			src: `Widget b() => TextFormField(
  keyboardType: TextInputType.phone,
);`,
		},
		{
			name: "a wrapper forwarding a variable declares no numeric intent of its own",
			src: `Widget b() => TextFormField(
  keyboardType: keyboardType,
);`,
		},
		{
			name: "a string literal naming the arguments is data, not a declaration",
			src:  `Widget b() => Text('keyboardType: TextInputType.number, validator: null');`,
		},
		{
			name: "a commented-out field is not a field",
			src: `Widget b() => Column(
  // TextField(keyboardType: TextInputType.number)
  children: const <Widget>[],
);`,
		},
		{
			name: "a conditional expression's colon is not an argument label",
			src: `Widget b() => TextFormField(
  keyboardType: TextInputType.number,
  style: dense ? small : large,
  validator: rule.validate,
);`,
		},
		{
			name: "a field DECLARATION is not an invocation",
			src: `class X {
  final TextInputType keyboardType = TextInputType.number;
}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkSource(t, tc.src); len(got) != 0 {
				t.Fatalf("want no findings, got %d: %+v", len(got), got)
			}
		})
	}
}

func TestAnchorsTheFindingOnTheInvocation(t *testing.T) {
	src := `Widget b() {
  return Column(
    children: [
      TextField(
        keyboardType: TextInputType.number,
      ),
    ],
  );
}`
	got := checkSource(t, src)
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d", len(got))
	}
	if got[0].Line != 4 {
		t.Errorf("finding anchored at line %d, want 4 (the TextField invocation)", got[0].Line)
	}
	if !strings.Contains(got[0].Message, "TextField") ||
		!strings.Contains(got[0].Message, "no validator") {
		t.Errorf("message does not name the widget and what is missing: %q", got[0].Message)
	}
}

func TestNonDartFileYieldsNothing(t *testing.T) {
	c := newChecker(t, defaultParams)
	f := scan.NewMemFile("x.go", []byte("keyboardType: TextInputType.number"))
	got, err := c.CheckFile(f)
	if err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want no findings for a non-Dart file, got %+v", got)
	}
}

// An argument list that never closes is a parse or projection failure. It must
// surface as an engine error, never as a silent pass (spec §11) — a rule that
// answers "no findings" to source it could not read is worse than no rule.
func TestUnterminatedArgumentListIsAnError(t *testing.T) {
	c := newChecker(t, defaultParams)
	f := scan.NewMemFile(
		"packages/feature_foo/lib/presentation/x.dart",
		[]byte("Widget b() => TextField(\n  keyboardType: TextInputType.number,\n"),
	)
	if _, err := c.CheckFile(f); err == nil {
		t.Fatal("want an error for an unterminated argument list, got a silent pass")
	}
}

func TestRequiredParams(t *testing.T) {
	var node yaml.Node
	if err := yaml.Unmarshal([]byte("keyboard_arg: keyboardType\n"), &node); err != nil {
		t.Fatalf("bad yaml: %v", err)
	}
	if _, err := newNumericFieldValidated(node.Content[0]); err == nil {
		t.Fatal("want an error when params.numeric_value is absent, got none")
	}
}

func TestRuleTypeIsRegistered(t *testing.T) {
	if _, ok := rules.Lookup("dart/numeric-field-validated"); !ok {
		t.Fatalf("dart/numeric-field-validated is not registered; its rule cannot load. registered: %v",
			rules.TypeNames())
	}
}

// A CLOSURE is a predicate written at the field, whatever its body syntax, and
// a reference never contains one. Bounding nesting depth or forbidding a
// character class cannot express that: `wrap((_) => null)` is a lambda one
// level in, and `Validators.of((v) { return null; })` is the same dodge with a
// block body and no fat arrow anywhere. Both are the shape the whitelist
// exists to reject — a predicate free to disagree with the write path it sits
// beside — and both passed every earlier spelling of the source pattern.
func TestFiresOnWrappedClosureValidators(t *testing.T) {
	for _, validator := range []string{
		`(_) => null`,
		`(v) => tryParse(v) == null ? 'Bad' : null`,
		`wrap((_) => null)`,
		`Validators.of((v) => tryParse(v) == null ? 'Bad' : null)`,
		`Validators.of((v) { return null; })`,
		`wrap((v) { if (v is! String) return 'Bad'; return null; })`,
	} {
		t.Run(validator, func(t *testing.T) {
			got := checkSource(t, `Widget build(BuildContext c) {
  return Field(
    keyboardType: const TextInputType.number,
    validator: `+validator+`,
  );
}`)
			if len(got) != 1 {
				t.Fatalf("closure validator %q accepted as a named predicate; want 1 match, got %d", validator, len(got))
			}
		})
	}
}

// The complement, and the reason the discriminator cannot be a character
// blacklist: a REFERENCE may contain any operator in its arguments. Forbidding
// `=` to catch the fat arrow flagged the seam's own builder the moment a
// comparison appeared in a bound.
func TestAcceptsReferencesCarryingOperators(t *testing.T) {
	for _, validator := range []string{
		`rule.validate`,
		`_gstRule`,
		`field.validator?.validate`,
		`framingInputRule(input).validate`,
		`NumericFieldRule.of(context, min: 0, max: max(1, n)).validate`,
		`NumericFieldRule.of(context, max: n >= 1 ? n : 1).validate`,
		`ServerValidationRules.of(ref, kind: k == Kind.a ? x : y).validate`,
		`NumericFieldRule.of(context, max: cap(ref.watch(x))).validate`,
		`rule.copyWith(max: cap ?? 10).validate`,
	} {
		t.Run(validator, func(t *testing.T) {
			got := checkSource(t, `Widget build(BuildContext c) {
  return Field(
    keyboardType: const TextInputType.number,
    validator: `+validator+`,
  );
}`)
			if len(got) != 0 {
				t.Fatalf("named predicate %q flagged as open-coded at the field: %+v", validator, got)
			}
		})
	}
}
