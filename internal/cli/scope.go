package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
)

// Split out of cli.go because this change would have taken it over the repo's
// own 750-line hard cap (file-size-vendor-cap) — measured, the unsplit file
// lands around 820. cli.go itself never went over: 738 lines before the split,
// 680 after. The split is what kept it there. runScope moved here and CHANGED on the way — it gained
// the empty-changeset arm and now acquires its paths through changesetFor — and
// the three helpers below were written here rather than moved: everyLanguage
// replaces a loop that was inline in the old runScope, and scopeEmptyPrefix and
// emptyChangesetCause are new with the empty arm. The shared acquisition seam
// went to changeset.go instead, because runCheck calls that one too.

// runScope classifies a file set as docs|governance|runtime and emits
// per-language change flags (spec §8). The file set comes from one of two
// questions, and the operand decides which:
//
//	formwork scope <path>...              the paths named on the command line
//	formwork scope [--staged|--range A..B] the changeset git reports
//
// Both were always documented (docs/reference.md:481 for the first, AGENTS.md
// for the second) and only the second existed: fs.Args() was never read, so
// every `formwork scope <path>` answered about the staged set instead (#288).
//
// The two modes differ in what an unusable answer means, and that difference is
// the whole reason the operand arm is not routed through changesetFor. A
// CHANGESET is acquired — git may fail, or name nothing — so once config is
// loaded that arm is fail-closed (spec §11): a git error OR an empty changeset
// (#147) yields class=runtime with every language flagged changed, exit 0. The
// classifier informs gating, it does not itself gate. PATH OPERANDS are given,
// not acquired: there is no git call to fail and no emptiness to assume around,
// so the answer is computed, never assumed, and the two fail-closed arms below
// are unreachable from it. What CAN be wrong is the question — a path outside
// the root, a directory, a flag the parser never saw — and each of those is
// exit 2 rather than a confident class, because a wrong answer to a routing
// query is the guidance fail-open docs/reference.md's Introspection preface
// names.
//
// All of that has always been conditional on config loading successfully:
// scope loads config through loadGated, as check, test, lint, hooks and the
// introspection commands do, and a config error there (malformed YAML, an
// unknown field, an unsatisfied `engine:` constraint) has always been exit 2
// with no stdout, from before the engine gate existed — a config that cannot be
// loaded at all was never something the classifier could produce a class for.
// The engine-version gate (finding 10) does not change this contract, only adds
// one more reason a config can fail to load.
//
// Flags are validated before config content, the ordering cli.go's
// --staged/--range guard already pins for check: a caller who typed two
// conflicting flags must be told about their flags, not about their config.
// That is also why the operand normalizer runs before loadGated — refusing a
// path outside the repository root cannot depend on whether the repository's
// rules parsed.
func runScope(args []string, stdout, stderr io.Writer) int {
	fs, root := newFlagSet("scope", "repository root", stderr)
	staged := fs.Bool("staged", false, "classify the staged changeset (default when --range is absent)")
	rangeSpec := fs.String("range", "", "classify a git range, e.g. origin/main..HEAD")
	format := fs.String("format", "human", "output format: human | json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !introspectFormat(*format, stderr) {
		return 2
	}
	if *staged && *rangeSpec != "" {
		fmt.Fprintln(stderr, "formwork: --staged and --range are mutually exclusive")
		return 2
	}
	// An empty --range is a SUPPLIED flag that silently becomes a different run:
	// it falls through to the staged set, which in CI is empty. The empty arm
	// below now catches that landing loudly — runtime, announced on stderr
	// — so this is no longer a fail-open. It is a wrong answer to the question
	// the operator actually asked, and the refusal names a cure the fallback
	// cannot. Verified by removing this guard: the run lands on the empty arm at
	// class=runtime, not the class=docs an earlier version of this comment
	// claimed (claim-auditor, 2026-08-19).
	if !rangeValueUsable(fs, *rangeSpec, "classifying the staged set instead", stderr) {
		return 2
	}
	paths, ok := scopeOperands(fs, *root, *staged, *rangeSpec, stderr)
	if !ok {
		return 2
	}
	cfg, ok := loadGated(*root, stderr)
	if !ok {
		return 2
	}
	if len(paths) > 0 {
		class, langs := cfg.Scope.Classify(paths)
		return emitScope(stdout, stderr, *format, cfg, scopeOut{Class: class, Languages: langs})
	}
	// anyStatus, not scannableOnly: scope classifies a change, it never reads
	// one, so a deleted source file counts. See changesetStatuses.
	changed, gitErr := changesetFor(*root, *staged, *rangeSpec, anyStatus)
	class, langs := cfg.Scope.Classify(changed)
	assumed := ""
	switch {
	case gitErr != nil:
		fmt.Fprintln(stderr, "formwork: scope: assuming runtime:", gitErr)
		assumed = fmt.Sprintf("git could not answer: %v", gitErr)
		class, langs = "runtime", everyLanguage(cfg)
	case len(changed) == 0:
		// Classify said `docs` here, correctly — docs IS the classification of
		// zero paths, which is why the guard is not in Classify: it is a pure
		// function over strings and did not fetch the set it is judging.
		//
		// The judgement that belongs HERE, at the seam that fetched it, is that
		// an empty answer is unusable for routing. runScope cannot tell a
		// genuinely-empty changeset from one emptied spuriously, and the two
		// costs are not symmetric: a wasted runtime lane is minutes, a skipped
		// one is the shape #99 exploits. So it assumes the strongest class, and
		// SAYS SO — an assumed classification that looks identical to a computed
		// one is how this went unnoticed at exit 0.
		//
		// A path operand cannot reach here: it names a non-empty file set that
		// no git call had to produce, so there is nothing to assume about.
		fmt.Fprintln(stderr, scopeEmptyPrefix, emptyChangesetCause(*rangeSpec))
		assumed = "empty changeset: " + emptyChangesetCause(*rangeSpec)
		class, langs = "runtime", everyLanguage(cfg)
	}
	return emitScope(stdout, stderr, *format, cfg, scopeOut{Class: class, Languages: langs, Assumed: assumed})
}

// scopeOperands resolves `formwork scope`'s positional arguments into
// root-relative paths, or refuses the invocation. An empty result with ok=true
// means no operand was given and the changeset modes own the run — the form
// AGENTS.md documents, and the only form the binary understood before #288.
//
// Three refusals, all of them exit 2 rather than a class, because scope's
// output is routing input: a wrapper acts on `class=` and cannot tell a
// computed answer from an answer to a question it did not ask.
func scopeOperands(fs *flag.FlagSet, root string, staged bool, rangeSpec string, stderr io.Writer) ([]string, bool) {
	args := fs.Args()
	if len(args) == 0 {
		return nil, true
	}
	// Go's flag package stops parsing at the first non-flag argument, so
	// anything flag-shaped standing among the operands never reached the flag
	// set — it was DISCARDED, and the run silently became a different run. That
	// is the amplification #288's refuters found: `scope docs/b.md --range
	// bogusref..HEAD` classified the staged set at exit 0 with a clean stderr,
	// while `scope --range bogusref..HEAD` fail-closed loudly with git's error,
	// so the documented operand form converted a fail-closed git error into a
	// silent answer.
	//
	// Refusing is the only fail-closed reading. Honouring it would mean
	// re-ordering argv behind the operator's back — and a shell that already
	// expanded a glob into this position makes that a guess about intent — while
	// ignoring it is exactly the defect. A file whose name really does begin
	// with "-" is still reachable, spelled "./-x", which is the same cure every
	// other tool gives.
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			fmt.Fprintf(stderr, "formwork: scope: %s follows a path operand, where flag parsing has already stopped — it would be discarded rather than applied, and the run would silently classify the changeset instead; put every flag before the first path (a path whose own name begins with \"-\" is spelled with a leading \"./\")\n", a)
			return nil, false
		}
	}
	// A selector and an operand are two different questions — "what changed?"
	// and "what class is this file?" — and answering either silently is a wrong
	// answer to the other.
	selector := ""
	switch {
	case staged:
		selector = "--staged"
	case rangeSpec != "":
		selector = "--range"
	}
	if selector != "" {
		fmt.Fprintf(stderr, "formwork: scope: %s selects a changeset, and a path operand names its own file set — these are different questions and combining them would answer only one; drop %s, or drop the paths\n", selector, selector)
		return nil, false
	}
	// The same normalizer rules-for queries through, for the same reason: an
	// out-of-root path, or a directory whose files may fall in different
	// classes, must be LOUD. A confident class for a path in a frame the
	// classifier never uses is the guidance fail-open the Introspection preface
	// promises exit 2 for — and scope was the one command in that section not
	// keeping the promise.
	norm := newQueryNormalizer(root)
	paths := make([]string, 0, len(args))
	for _, a := range args { // argv order preserved; Classify is order-free
		p, err := norm.normalize(a)
		if err != nil {
			fmt.Fprintf(stderr, "formwork: %v\n", err)
			return nil, false
		}
		paths = append(paths, p)
	}
	return paths, true
}

// scopeOut is the `-format json` wire shape (#330). Languages carries EVERY
// declared language, not only the changed ones, so a consumer keying off
// languages["dart"] can tell false from absent — the human format has the same
// property and it is the half a router depends on.
//
// Assumed is the JSON half of scope's assumed-vs-computed distinction. The
// fail-closed arms are exit 0 with a confident-looking class and say so only on
// stderr, which json.Unmarshal does not read; without this key the machine
// surface would reproduce, for the consumer least able to notice, exactly the
// defect the stderr line exists to close. Its PRESENCE is the contract (absent
// when the class was computed, per omitempty); the reason text inside it is a
// message and free to improve.
type scopeOut struct {
	Class     string          `json:"class"`
	Languages map[string]bool `json:"languages"`
	Assumed   string          `json:"assumed,omitempty"`
}

// emitScope writes the classification. One writer for both modes and both
// formats: an operand answer and a changeset answer are the same contract, and
// a consumer must not be able to tell from the SHAPE of stdout which question
// was asked.
//
// The human arm walks cfg.Scope.Languages rather than the map, because
// declaration order is the order operators read; the JSON arm hands the map to
// encoding/json, which sorts keys, so both are deterministic and diffable.
func emitScope(stdout, stderr io.Writer, format string, cfg *config.Config, out scopeOut) int {
	if format == "json" {
		return emitJSON(stdout, stderr, out)
	}
	fmt.Fprintf(stdout, "class=%s\n", out.Class)
	for _, l := range cfg.Scope.Languages {
		fmt.Fprintf(stdout, "%s_changed=%t\n", l.Name, out.Languages[l.Name])
	}
	return 0
}

// scopeEmptyPrefix is the operator-visible contract for scope's EMPTY arm. It
// is deliberately not a prefix of, and does not contain, the git-error line one
// branch up: the two
// need different next actions — that one says the git call failed, this one says
// git answered and named nothing — and a wrapper grepping stderr must be able to
// tell them apart. The text after it is free to change; this string is not.
const scopeEmptyPrefix = "formwork: scope: empty changeset — assuming runtime:"

// emptyChangesetCause names which selector came back empty and what to do about
// it, because the cure differs per mode and this line is the whole cure surface.
func emptyChangesetCause(rangeSpec string) string {
	if rangeSpec != "" {
		return fmt.Sprintf("%q named no changed paths; if that range was built from a base ref, check it resolved to what you meant, then re-run", rangeSpec)
	}
	return "nothing is staged; stage the change you meant to classify, or pass --range with a base that exists"
}

// everyLanguage flags every declared language as changed — the language half of
// an assumed classification, which must be as wide as the class it accompanies.
func everyLanguage(cfg *config.Config) map[string]bool {
	langs := make(map[string]bool, len(cfg.Scope.Languages))
	for _, l := range cfg.Scope.Languages {
		langs[l.Name] = true
	}
	return langs
}
