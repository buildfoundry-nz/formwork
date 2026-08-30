package dartscan

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// Defaults for the two argument names. They are params rather than constants
// so the rule states its own contract in the YAML instead of hiding it in Go,
// but no deployment is expected to need different ones.
const (
	defaultKeyboardArg  = "keyboardType"
	defaultValidatorArg = "validator"
	// defaultValidatorSource is the shape of a NAMED predicate: an identifier
	// path (`rule.validate`, `ServerValidationRules.validateOptionalFloorArea`,
	// a bare `_gstRule`, a null-aware `field.validator?.validate`), optionally
	// called to build one (`framingInputRule(input)`).
	//
	// A WHITELIST, not a blacklist of inert spellings. Naming the dodges only
	// ever names the ones already thought of: `validator: null` and
	// `(_) => null` were the two on record, and `(v) => tryParse(v) == null ?
	// 'Bad' : null` would have sailed past both while being the actual
	// regrowth path — a predicate written at the field instead of declared as
	// a rule beside it, free to disagree with the write path it sits next to.
	defaultValidatorSource = `^[A-Za-z_][A-Za-z0-9_]*(\((?:[^()]|\((?:[^()]|\([^()]*\))*\))*\))?(\??\.[A-Za-z_][A-Za-z0-9_]*(\((?:[^()]|\((?:[^()]|\([^()]*\))*\))*\))?)*$`
)

type numericFieldValidatedParams struct {
	NumericValue    string `yaml:"numeric_value"`
	KeyboardArg     string `yaml:"keyboard_arg"`
	ValidatorArg    string `yaml:"validator_arg"`
	ValidatorSource string `yaml:"validator_source"`
}

// numericFieldValidated flags a constructor invocation that DECLARES ITSELF
// numeric — it passes a `keyboardType:` whose value matches NumericValue — but
// supplies no live `validator:`.
//
// The invariant is a structural one over the argument list, not a lexical one
// over the file: "a numeric text input routes through a validator". The
// lexical version of this shape does not converge (#8677). `keyboardType` and
// `validator` sit in the SAME argument list, arbitrarily far apart in the
// source, interleaved with nested invocations that carry argument names of
// their own — `TextInputType.numberWithOptions(decimal: true)` is itself an
// argument list one level down. A line- or window-scoped pattern cannot tell
// an argument of THIS invocation from an argument of a nested one, and cannot
// tell an absent validator from one that is merely far away.
//
// So the checker walks bracket depth from the invocation's opening paren and
// reads only the arguments at depth 1: the ones this invocation actually
// receives. That also means the rule sees through the repo's wrapper widgets
// (JobFormAiTextField, EditableFieldInput, AppField), which take a
// `keyboardType` and forward it to an inner field. The wrapper's CALL SITE is
// where the numeric intent is declared, so that is where the validator is
// required — a wrapper that forwards a variable `keyboardType` declares no
// numeric intent of its own and is not flagged.
//
// Matching any Constructor-shaped callee rather than a hard-coded widget list
// is deliberate: `TextField` has no `validator` parameter at all, so the cure
// for a numeric one is to become a `TextFormField`, and a rule keyed to a
// closed set of widget names would be silent the moment a fifth site wrapped
// its field in a new widget — the regrowth #9925 exists to prevent.
type numericFieldValidated struct {
	numericValue    *regexp.Regexp
	keyboardArg     string
	validatorArg    string
	validatorSource *regexp.Regexp
}

func newNumericFieldValidated(node *yaml.Node) (rules.Checker, error) {
	var p numericFieldValidatedParams
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	if p.NumericValue == "" {
		return nil, errors.New("dart/numeric-field-validated: params.numeric_value is required")
	}
	numericValue, err := regexp.Compile(p.NumericValue)
	if err != nil {
		return nil, fmt.Errorf("dart/numeric-field-validated: invalid numeric_value regex: %w", err)
	}
	if p.KeyboardArg == "" {
		p.KeyboardArg = defaultKeyboardArg
	}
	if p.ValidatorArg == "" {
		p.ValidatorArg = defaultValidatorArg
	}
	if p.ValidatorSource == "" {
		p.ValidatorSource = defaultValidatorSource
	}
	source, err := regexp.Compile(p.ValidatorSource)
	if err != nil {
		return nil, fmt.Errorf("dart/numeric-field-validated: invalid validator_source regex: %w", err)
	}
	return &numericFieldValidated{
		numericValue:    numericValue,
		keyboardArg:     p.KeyboardArg,
		validatorArg:    p.ValidatorArg,
		validatorSource: source,
	}, nil
}

func (c *numericFieldValidated) CheckFile(f *scan.File) ([]rules.Match, error) {
	if !isDart(f) {
		return nil, nil
	}
	content, err := f.Content()
	if err != nil {
		return nil, err
	}
	src := string(content)
	// Nothing in this file names the keyboard argument, so no invocation in it
	// can declare numeric intent. Skips the argument-list walk for the ~97% of
	// the Dart corpus that has no numeric field at all.
	if !strings.Contains(src, c.keyboardArg) {
		return nil, nil
	}

	var matches []rules.Match
	for _, inv := range invocations(src) {
		args, err := namedArgs(src, inv.open)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %s(: %w", f.Path(), lineAt(src, inv.nameStart), inv.name, err)
		}
		keyboard, ok := args[c.keyboardArg]
		if !ok || !c.numericValue.MatchString(keyboard) {
			continue
		}
		validator, ok := args[c.validatorArg]
		// A validator spanning lines is one expression to the walk but arrives
		// with the source's newlines and indentation in it; the anchored
		// source pattern would never match that. Collapsed first so a
		// dart-format wrap cannot decide whether a field passes.
		if ok && isNamedPredicate(collapseSpace(validator), c.validatorSource) {
			continue
		}
		why := fmt.Sprintf("supplies no %s", c.validatorArg)
		if ok {
			why = fmt.Sprintf(
				"supplies a %s that is open-coded at the field rather than a named predicate (%s)",
				c.validatorArg, collapseSpace(validator))
		}
		matches = append(matches, rules.Match{
			Line: lineAt(src, inv.nameStart),
			Message: fmt.Sprintf("%s(...) declares a numeric %s (%s) but %s",
				inv.name, c.keyboardArg, keyboard, why),
		})
	}
	return matches, nil
}

// invocation is one `Name(` / `Name<T>(` site: the callee's name, the offset
// its identifier starts at, and the offset of its opening paren.
type invocation struct {
	name      string
	nameStart int
	open      int
}

// invocations finds every constructor-shaped call in src — an identifier whose
// first letter (after an optional leading `_`) is upper case, optionally
// followed by type arguments, then `(`. Nested calls are returned too: the
// scan continues INSIDE a matched argument list rather than skipping it, so
// `Row(children: [TextFormField(...)])` yields both.
func invocations(src string) []invocation {
	var out []invocation
	n := len(src)
	for i := 0; i < n; i++ {
		if !isIdentStart(src[i]) {
			continue
		}
		start := i
		j := i
		for j < n && isIdentByte(src[j]) {
			j++
		}
		ident := src[start:j]
		// Advance the outer scan past the identifier either way — an
		// identifier's interior can never start another one.
		i = j - 1
		if !constructorShaped(ident) {
			continue
		}
		// A named-argument label (`validator:`) and a declaration
		// (`final TextInputType? keyboardType;`) are not calls; only a `(`
		// (optionally behind type arguments) makes this an invocation.
		k := skipTypeArgs(src, j)
		if k < n && src[k] == '(' {
			out = append(out, invocation{name: ident, nameStart: start, open: k})
		}
	}
	return out
}

// constructorShaped reports whether ident names a class/widget rather than a
// local or a method: an optional leading underscore then an upper-case letter.
// Private widgets (`_ThresholdInput`) count; `double.tryParse` does not.
func constructorShaped(ident string) bool {
	s := strings.TrimPrefix(ident, "_")
	if s == "" {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z'
}

// skipTypeArgs returns the offset of the first byte after a balanced `<…>`
// type-argument list starting at or after i (whitespace-tolerant), or i itself
// when there is none. Only the bytes a type argument can contain are accepted,
// so a `<` that is really a comparison operator ends the skip immediately and
// the caller sees no invocation.
func skipTypeArgs(src string, i int) int {
	j := skipSpace(src, i)
	if j >= len(src) || src[j] != '<' {
		return j
	}
	depth := 0
	for k := j; k < len(src); k++ {
		switch ch := src[k]; {
		case ch == '<':
			depth++
		case ch == '>':
			depth--
			if depth == 0 {
				return skipSpace(src, k+1)
			}
		case isIdentByte(ch) || ch == ',' || ch == '.' || ch == '?' || ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			// still plausibly a type argument
		default:
			return i
		}
	}
	return i
}

// namedArgs reads the argument list whose `(` is at open and returns the
// DEPTH-1 named arguments: label -> trimmed value text. Arguments of nested
// invocations live at depth 2 and are invisible here, which is the whole point
// — `TextInputType.numberWithOptions(decimal: true)` must not contribute a
// `decimal` argument to the field that receives it.
//
// A label is an identifier followed by `:` in ARGUMENT-START position (right
// after the `(` or after a depth-1 comma). Requiring argument-start is what
// keeps a conditional expression's colon — `dense ? a : b` — from reading as a
// label.
//
// An argument list that never closes is a projection or parse failure and is
// returned as an error (engine exit 2), never a silent pass.
func namedArgs(src string, open int) (map[string]string, error) {
	args := map[string]string{}
	n := len(src)
	depth := 0
	atArgStart := false
	label := ""
	valueStart := -1

	flush := func(end int) {
		if label != "" && valueStart >= 0 {
			args[label] = strings.TrimSpace(src[valueStart:end])
		}
		label, valueStart = "", -1
	}

	for i := open; i < n; i++ {
		switch ch := src[i]; ch {
		case '(', '[', '{':
			depth++
			if depth == 1 {
				atArgStart = true
			}
		case ')', ']', '}':
			depth--
			if depth == 0 {
				flush(i)
				return args, nil
			}
		case ',':
			if depth == 1 {
				flush(i)
				atArgStart = true
			}
		default:
			if depth != 1 || !atArgStart {
				continue
			}
			if isSpace(ch) {
				continue
			}
			if label == "" && isIdentStart(ch) {
				j := i
				for j < n && isIdentByte(src[j]) {
					j++
				}
				k := skipSpace(src, j)
				if k < n && src[k] == ':' {
					label = src[i:j]
					valueStart = skipSpace(src, k+1)
					i = valueStart - 1
					atArgStart = false
					continue
				}
			}
			// A positional argument, or the value of a label already read:
			// either way this argument's label slot is settled.
			atArgStart = false
		}
	}
	return nil, errors.New("argument list never closes (unbalanced brackets)")
}

// isNamedPredicate reports whether expr names a predicate rather than being one.
//
// `null` is excluded in Go rather than in the pattern because it is a LANGUAGE
// fact, not a deployment's choice: the Dart null literal is lexically an
// identifier, so any identifier-shaped source pattern admits it, and RE2 has no
// negative lookahead to write the exception with. Leaving it to the pattern
// would mean every deployment had to remember to exclude it, and the one that
// forgot would accept `validator: null` as routing through the seam.
func isNamedPredicate(expr string, source *regexp.Regexp) bool {
	if expr == "null" {
		return false
	}
	if containsClosure(expr) {
		return false
	}
	return source.MatchString(expr)
}

// containsClosure reports whether expr contains a Dart function literal — an
// argument list immediately followed by either `=>` or a block body.
//
// This is in Go for the same reason `null` is: it is a LANGUAGE fact, not a
// deployment's choice. A closure is a predicate written at the field; a
// reference to a declared rule never contains one. No source pattern can carry
// the distinction:
//
//   - bounding NESTING DEPTH admits the dodge as soon as it is wrapped, since
//     `wrap((_) => null)` is a lambda one level in;
//   - forbidding the FAT ARROW misses `(v) { return null; }` entirely — a
//     block-bodied closure has no arrow — and, because RE2 has no negative
//     lookahead, the nearest expressible form bans `=` outright, which flags
//     every legitimate builder whose bound carries `>=`, `==` or `!=`.
//
// Both halves of "a reference never contains an arrow and a predicate always
// does" are false. What holds is that a reference never contains a CLOSURE,
// and that is depth-independent and body-syntax-independent.
//
// expr arrives collapsed to single spaces, so `) =>` and `) {` are the only
// spacings to consider.
func containsClosure(expr string) bool {
	for i := 0; i < len(expr); i++ {
		if expr[i] != ')' {
			continue
		}
		j := i + 1
		for j < len(expr) && expr[j] == ' ' {
			j++
		}
		if j < len(expr) && expr[j] == '{' {
			return true
		}
		if j+1 < len(expr) && expr[j] == '=' && expr[j+1] == '>' {
			return true
		}
	}
	return false
}

// collapseSpace squeezes every run of whitespace to a single space and trims
// the ends, so a wrapped expression compares as the one line it means.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentByte(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func skipSpace(src string, i int) int {
	for i < len(src) && isSpace(src[i]) {
		i++
	}
	return i
}

// lineAt returns the 1-based line number of byte offset off.
func lineAt(src string, off int) int {
	if off > len(src) {
		off = len(src)
	}
	return strings.Count(src[:off], "\n") + 1
}

func init() {
	rules.Register("dart/numeric-field-validated", newNumericFieldValidated)
}
