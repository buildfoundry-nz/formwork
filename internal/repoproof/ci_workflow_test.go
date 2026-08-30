// ci_workflow_test.go — CI installs the Go version go.mod's `toolchain`
// directive names, so setup-go's module cache can actually restore (#282).
//
// WHY THIS EXISTS. go.mod carries `go 1.25.0` and `toolchain go1.26.4`.
// actions/setup-go@v5 reads only the `go` directive from `go-version-file:
// go.mod`, so it installed 1.25.0 — and the very next thing it does is shell
// out to `go env GOMODCACHE` from inside the checkout, which makes that 1.25.0
// honour the `toolchain` directive and download go1.26.4 INTO GOMODCACHE. The
// module-cache restore then untars a cache containing those same
// golang.org/toolchain@... paths over the copy that was written a second
// earlier, every path collides, and tar exits 2:
//
//	23:09:40.83  go: downloading go1.26.4 (linux/amd64)
//	23:09:40.85  [command] .../go env GOMODCACHE
//	23:09:41.09  Cache hit for: setup-go-Linux-x64-ubuntu24-go-1.26.4-67c79a5a...
//	23:09:42.27  [command]/usr/bin/tar -xf .../cache.tzst -P -C .../formwork
//	23:09:42.34  /usr/bin/tar: ../../../go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.4.linux-amd64/...: Cannot open: File exists
//	23:09:44.47  ##[warning]Failed to restore: "/usr/bin/tar" failed ... exit code 2
//	23:09:44.48  Cache is not found
//
// That is run 32909467430 — a fully GREEN run on main — where it hit all three
// jobs across two runner vendors, 11,534 collisions each.
//
// STATED HONESTLY, because the warning reads worse than it is: every colliding
// path is inside `golang.org/toolchain@`, and the only `go: downloading` line
// in that run is the toolchain, so tar skips them, extracts the rest, and the
// dependency cache lands warm. The real cost is that setup-go concludes "Cache
// is not found", so every job re-tars the whole module and build cache at
// post-job and is then refused ("Failed to save: Unable to reserve cache with
// key ..."), plus a standing warning that teaches readers to ignore cache
// warnings, over a tar that exits 2 and would lose any future overlap outside
// the toolchain subtree in the same breath.
//
// #183 read this same symptom as evidence that ONE runner was degraded. It is
// neither degradation nor runner-specific: on attempt 1 of the run it cites,
// all three jobs failed the restore identically across three runner vendors,
// and it followed the move to Blacksmith unchanged.
//
// The fix is to hand setup-go the version the `toolchain` directive names, so
// nothing downloads a toolchain into GOMODCACHE before the restore. Verified
// locally against both toolchains with a throwaway GOMODCACHE and GOPROXY=off:
//
//	go1.25.13 + `toolchain go1.26.4` -> "go: downloading go1.26.4 (darwin/arm64)"
//	go1.26.4  + `toolchain go1.26.4` -> prints the path, downloads nothing
//
// WHAT THIS PINS, and why it is a test rather than a rule. The property is not
// "the file contains a string" — it is "the version the workflow hands to
// setup-go is the one go.mod's toolchain directive names", which needs both
// files parsed and the action's own resolver EXECUTED against the real go.mod.
// A forbidden-pattern rule cannot do either.
package repoproof_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// setupGoActionPath is the single place this repository configures Go in CI.
const setupGoActionPath = ".github/actions/setup-go"

// wfStep is one step of a workflow job or of a composite action.
type wfStep struct {
	ID   string         `yaml:"id"`
	Name string         `yaml:"name"`
	Uses string         `yaml:"uses"`
	Run  string         `yaml:"run"`
	With map[string]any `yaml:"with"`
}

// with returns the string form of a `with:` input, and whether it was present.
// The map is any-valued deliberately: `cache: true` decodes as a bool, and a
// map[string]string would make the whole file undecodable rather than reporting
// on the key asked about.
func (s wfStep) with(key string) (string, bool) {
	v, ok := s.With[key]
	if !ok {
		return "", false
	}
	if str, isStr := v.(string); isStr {
		return str, true
	}
	return "", true
}

type wfJob struct {
	Name string `yaml:"name"`
	// Needs is `any` because GitHub accepts a bare job id or a list of them.
	Needs    any `yaml:"needs"`
	RunsOn   any `yaml:"runs-on"`
	Strategy struct {
		Matrix struct {
			Include []map[string]any `yaml:"include"`
		} `yaml:"matrix"`
	} `yaml:"strategy"`
	Steps []wfStep `yaml:"steps"`
}

type workflow struct {
	Name string           `yaml:"name"`
	On   map[string]any   `yaml:"on"`
	Jobs map[string]wfJob `yaml:"jobs"`
}

type compositeAction struct {
	Runs struct {
		Using string   `yaml:"using"`
		Steps []wfStep `yaml:"steps"`
	} `yaml:"runs"`
}

// workflowPaths lists every tracked workflow, sorted, failing closed when the
// directory holds none — a gate that judges an empty set must not report a pass.
func workflowPaths(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join(repoRoot(t), ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ext := filepath.Ext(e.Name()); ext != ".yml" && ext != ".yaml" {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatalf("no workflow files under %s — this gate has nothing to judge", dir)
	}
	return out
}

func readWorkflow(t *testing.T, path string) workflow {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	var wf workflow
	if err := yaml.Unmarshal(b, &wf); err != nil {
		t.Fatalf("cannot parse %s: %v", path, err)
	}
	if len(wf.Jobs) == 0 {
		t.Fatalf("%s declares no jobs — this gate has nothing to judge", path)
	}
	return wf
}

// jobIDs returns a workflow's job ids in a stable order, so a failure message
// reads the same on every run.
func jobIDs(wf workflow) []string {
	ids := make([]string, 0, len(wf.Jobs))
	for id := range wf.Jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// goModDirective returns the version named by go.mod's `toolchain` directive,
// falling back to the `go` directive when there is no toolchain line. That
// fallback is the whole point: it is the version setup-go must be given, and
// the two coincide exactly when the collision this file pins cannot happen.
func goModDirective(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "go.mod"))
	if err != nil {
		t.Fatalf("cannot read go.mod: %v", err)
	}
	var goLine string
	for _, l := range strings.Split(string(b), "\n") {
		fields := strings.Fields(l)
		if len(fields) < 2 || strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") {
			continue
		}
		switch fields[0] {
		case "toolchain":
			return strings.TrimPrefix(fields[1], "go")
		case "go":
			if goLine == "" {
				goLine = fields[1]
			}
		}
	}
	if goLine == "" {
		t.Fatal("go.mod names neither a toolchain nor a go directive")
	}
	return goLine
}

// isSetupGoAction reports whether a step's `uses:` names an action that installs
// Go, whoever publishes it.
//
// It is deliberately not `HasPrefix("actions/setup-go")`. Linux runs on
// Blacksmith here, and `useblacksmith/setup-go` is published as a drop-in
// replacement for exactly this step, marketed on faster Go caching — which is
// the first thing a maintainer chasing #282's cache-restore warning reaches
// for. Dropped into a job with `go-version-file: go.mod` it reinstates #282
// exactly: the `go` directive's 1.25.0 gets installed, `go env` downloads
// go1.26.4 into GOMODCACHE, and the restore collides with it. A
// vendor-specific check reads that as a pass, and I confirmed against this
// tree that it did.
//
// Matching on the segment rather than the whole name is the point: it catches
// `useblacksmith/setup-go`, `setup-go-faster` and anything else named for the
// job it does, and it costs a false positive only for an action with
// "setup-go" in its name that does not set up Go.
func isSetupGoAction(uses string) bool {
	name := uses
	if i := strings.Index(name, "@"); i >= 0 {
		name = name[:i]
	}
	for _, seg := range strings.Split(name, "/") {
		if strings.Contains(strings.ToLower(seg), "setup-go") {
			return true
		}
	}
	return false
}

// stepInstallsGo reports whether one step of the composite action installs Go,
// and is the predicate every assertion about that action is filtered through.
//
// It asks isSetupGoAction — the same question the workflow-level check asks —
// rather than `HasPrefix("actions/setup-go")`. The vendor prefix judged the
// step this repository happens to use today and silently exempted every other
// one, in the single file where the Go version #282 turns on is chosen. A
// second setup-go step from another vendor could then be added beside the
// sanctioned one, carrying the `go-version-file: go.mod` this file forbids,
// and be judged by nothing.
func stepInstallsGo(st wfStep) bool {
	return isSetupGoAction(st.Uses)
}

// setupGoSteps returns the steps of the composite action that install Go, in
// declaration order. A caller that needs the surrounding order (to know which
// steps ran first) filters with stepInstallsGo directly instead.
func setupGoSteps(act compositeAction) []wfStep {
	var out []wfStep
	for _, st := range act.Runs.Steps {
		if stepInstallsGo(st) {
			out = append(out, st)
		}
	}
	return out
}

// plantedSecondVendorAction is the edit this gate exists to refuse, and it is
// not hypothetical: appended to the real .github/actions/setup-go/action.yml it
// left the WHOLE repoproof package green.
//
// The sanctioned resolver and its `actions/setup-go` step are still here and
// still correct — which is why every assertion in this file was satisfied. The
// second step is the one that decides: composite steps run in order, so the
// LAST setup-go to run is the one whose Go ends up on PATH, and this one is
// handed the `go-version-file: go.mod` that reads the `go` directive, installs
// 1.25.0, has `go env` download go1.26.4 into GOMODCACHE and collides with the
// module-cache restore. That is #282 restored in full.
//
// A const rather than a temporary file: the fixture is the assertion here, and
// it has to be readable next to the thing it condemns.
const plantedSecondVendorAction = `
runs:
  using: composite
  steps:
    - id: version
      shell: bash
      run: echo "go-version=1.26.4" >> "$GITHUB_OUTPUT"
    - uses: actions/setup-go@v5
      with:
        go-version: ${{ steps.version.outputs.go-version }}
        cache: true
    - uses: useblacksmith/setup-go@v6
      with:
        go-version-file: go.mod
        cache: true
`

// parseCompositeAction decodes a composite action written inline, so a gate
// over action.yml can be judged against a shape that is not in the tree. It
// fails closed on a fixture it cannot decode or one that declares no steps: a
// gate proved against nothing is the thing this file is about.
func parseCompositeAction(t *testing.T, src string) compositeAction {
	t.Helper()
	var act compositeAction
	if err := yaml.Unmarshal([]byte(src), &act); err != nil {
		t.Fatalf("cannot parse the planted composite action: %v", err)
	}
	if len(act.Runs.Steps) == 0 {
		t.Fatalf("the planted composite action decoded to no steps, so this gate judged nothing")
	}
	return act
}

// TestSetupGoActionGateJudgesEverySetupGoVendor pins the predicate the three
// assertions about action.yml are filtered through.
//
// TestWorkflowsSetUpGoOnlyThroughTheToolchainPinningAction stops a WORKFLOW JOB
// reaching any setup-go action directly, whoever publishes it. Nothing said the
// same about the composite action's own steps, and that is the file where the
// version is chosen — so a second setup-go step from another vendor, added
// beside the sanctioned one, was judged by none of the three tests that read it.
//
// The steps are compared by `uses:` rather than counted, so a predicate that
// accepted the right number of the wrong steps cannot pass, and the
// `go-version-file` assertion states the consequence directly: the input #282
// exists to forbid must be VISIBLE to the gate, not merely present in the file.
func TestSetupGoActionGateJudgesEverySetupGoVendor(t *testing.T) {
	act := parseCompositeAction(t, plantedSecondVendorAction)

	var got []string
	for _, st := range setupGoSteps(act) {
		got = append(got, st.Uses)
	}
	want := []string{"actions/setup-go@v5", "useblacksmith/setup-go@v6"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the gate over %s/action.yml judges these steps as Go setup\n  %s\nbut the "+
			"action declares\n  %s\nA step it does not judge is a step free to pass "+
			"`go-version-file: go.mod`, which is #282 exactly (#282).",
			setupGoActionPath, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}

	var sawGoVersionFile bool
	for _, st := range setupGoSteps(act) {
		if _, ok := st.with("go-version-file"); ok {
			sawGoVersionFile = true
		}
	}
	if !sawGoVersionFile {
		t.Errorf("no step this gate judges carries `go-version-file`, so the assertion that "+
			"forbids it in %s/action.yml never sees the step that has it. setup-go then "+
			"installs the `go` directive's version, `go env` downloads the toolchain into "+
			"GOMODCACHE, and the module-cache restore collides (#282).", setupGoActionPath)
	}
}

// TestWorkflowsSetUpGoOnlyThroughTheToolchainPinningAction is the structural
// half: Go is installed through one composite action and nowhere else, because
// that action is where the toolchain version is pinned. A job that reaches any
// setup-go action directly is free to pass `go-version-file: go.mod` again,
// which is precisely the configuration that breaks the restore.
func TestWorkflowsSetUpGoOnlyThroughTheToolchainPinningAction(t *testing.T) {
	var direct []string
	usesAction := false
	for _, path := range workflowPaths(t) {
		wf := readWorkflow(t, path)
		rel, _ := filepath.Rel(repoRoot(t), path)
		for _, id := range jobIDs(wf) {
			for _, st := range wf.Jobs[id].Steps {
				if !isSetupGoAction(st.Uses) {
					continue
				}
				if strings.TrimPrefix(st.Uses, "./") == setupGoActionPath {
					usesAction = true
					continue
				}
				direct = append(direct, rel+" job "+id+": uses "+st.Uses)
			}
		}
	}
	if len(direct) > 0 {
		t.Errorf("%d workflow step(s) install Go through a setup-go action of their own "+
			"instead of ./%s, so nothing stops them installing the `go` directive's "+
			"version and making the module-cache restore collide with the toolchain "+
			"download (#282). Switching vendors does not fix that collision; pinning "+
			"the version go.mod's toolchain directive names does:\n  %s",
			len(direct), setupGoActionPath, strings.Join(direct, "\n  "))
	}
	if !usesAction {
		t.Errorf("no workflow step uses ./%s — the pinning action is not on any CI path, "+
			"so it pins nothing", setupGoActionPath)
	}
}

// TestSetupGoActionInstallsGoExactlyOnce makes the action's own description
// true. It says "Go is installed here by exactly one step", and until this test
// that sentence was the same kind of claim #281 is about — appending a second,
// perfectly well-formed setup-go step to the real action.yml left the whole
// repository green.
//
// Two correct steps are not harmless. Composite steps run in order and the last
// Go on PATH wins, so which one the job actually uses stops being readable from
// the top of the file; the cache is restored and saved twice under two keys;
// and the shape is one input away from #282, since the version each step is
// given is then a separate decision. The file exists to make Go setup readable
// in one place, and "one place" is what this pins.
//
// It fails closed on zero as well, deliberately redundant with the two tests
// below that own that condition: an action that installs no Go at all is not a
// pass, it is a gate with nothing to judge.
func TestSetupGoActionInstallsGoExactlyOnce(t *testing.T) {
	steps := setupGoSteps(readSetupGoAction(t))
	if len(steps) == 1 {
		return
	}
	uses := make([]string, 0, len(steps))
	for _, st := range steps {
		uses = append(uses, st.Uses)
	}
	t.Errorf("%s/action.yml installs Go in %d steps (%s), and its own description says exactly "+
		"one. Composite steps run in order and the last Go on PATH wins, so which one the job "+
		"uses stops being readable here — and the version each is handed becomes a separate "+
		"decision, which is one input away from #282.",
		setupGoActionPath, len(steps), strings.Join(uses, ", "))
}

func readSetupGoAction(t *testing.T) compositeAction {
	t.Helper()
	path := filepath.Join(repoRoot(t), setupGoActionPath, "action.yml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s — CI has no single place that pins the Go toolchain version (#282): %v",
			filepath.ToSlash(filepath.Join(setupGoActionPath, "action.yml")), err)
	}
	var act compositeAction
	if err := yaml.Unmarshal(b, &act); err != nil {
		t.Fatalf("cannot parse %s: %v", path, err)
	}
	return act
}

// TestSetupGoActionPinsAVersionRatherThanTheGoModFile is the negative half: the
// action must NOT hand setup-go `go-version-file`, which is the input that
// reads the `go` directive and reintroduces #282.
func TestSetupGoActionPinsAVersionRatherThanTheGoModFile(t *testing.T) {
	act := readSetupGoAction(t)
	if act.Runs.Using != "composite" {
		t.Fatalf("%s/action.yml is not a composite action (runs.using = %q)", setupGoActionPath, act.Runs.Using)
	}
	var found bool
	for _, st := range setupGoSteps(act) {
		found = true
		if v, ok := st.with("go-version-file"); ok {
			t.Errorf("%s/action.yml hands setup-go `go-version-file: %s`. setup-go reads the "+
				"`go` directive from it and ignores `toolchain`, so it installs the wrong "+
				"toolchain and the module-cache restore collides (#282)", setupGoActionPath, v)
		}
		v, ok := st.with("go-version")
		if !ok {
			t.Errorf("%s/action.yml gives setup-go no `go-version`", setupGoActionPath)
			continue
		}
		if !strings.Contains(v, "steps.") {
			t.Errorf("%s/action.yml pins `go-version: %s` as a literal. It must come from the "+
				"resolver step so it cannot drift from go.mod", setupGoActionPath, v)
		}
	}
	if !found {
		t.Errorf("%s/action.yml never calls a setup-go action at all, so this gate judged nothing",
			setupGoActionPath)
	}
}

// yamlTrue reports whether a `with:` input decoded from YAML reads as on.
// Both spellings reach here: `cache: true` decodes as a bool, `cache: "true"`
// as a string, and a gate that understood only one of them would pass the
// other by accident rather than on purpose.
func yamlTrue(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(strings.TrimSpace(t), "true")
	default:
		return false
	}
}

// TestSetupGoActionKeepsSetupGosModuleCacheOn closes the cheapest wrong fix for
// #282. What a reader of a CI log sees is 11,534 tar collisions during the
// module-cache restore, and the one-line way to make that noise stop is
// `cache: false` — which deletes the cache instead of the collision and leaves
// every other assertion in this file green, because each of them is about
// which Go version gets installed. The restore only matters while there is
// something to restore, so whether the cache is on is pinned here.
//
// It is pinned as an EXPLICIT input, not merely as "not false": inherited from
// actions/setup-go's own default, whether this repo caches would be a fact
// about a third-party action version rather than a fact readable in this tree,
// and having the Go setup readable in one place is the entire reason the
// composite action exists.
func TestSetupGoActionKeepsSetupGosModuleCacheOn(t *testing.T) {
	act := readSetupGoAction(t)
	var found bool
	for _, st := range setupGoSteps(act) {
		found = true
		v, ok := st.With["cache"]
		if !ok {
			t.Errorf("%s/action.yml gives setup-go no `cache` input, so whether this repo caches "+
				"Go modules is whatever that action version defaults to. #282 is a defect in the "+
				"module-cache RESTORE; state here that there is a cache to restore.", setupGoActionPath)
			continue
		}
		if !yamlTrue(v) {
			t.Errorf("%s/action.yml hands setup-go `cache: %v`, which is not on. Turning the cache "+
				"off makes #282's restore warning disappear by removing the restore — the "+
				"collision is fixed by installing the toolchain version go.mod names, not by "+
				"giving up the cache.", setupGoActionPath, v)
		}
	}
	// Deliberately redundant with TestSetupGoActionPinsAVersionRatherThanTheGoModFile,
	// which owns this same condition: a gate that judges no steps at all must
	// report that, not a pass.
	if !found {
		t.Fatalf("%s/action.yml never calls a setup-go action, so this gate judged nothing", setupGoActionPath)
	}
}

// stepOutputRef matches one `${{ steps.<id>.outputs.<name> }}` expression, the
// only way a composite action step can read an earlier step's output.
var stepOutputRef = regexp.MustCompile(`\$\{\{[[:space:]]*steps\.([A-Za-z_][-A-Za-z0-9_]*)\.outputs\.([A-Za-z_][-A-Za-z0-9_]*)[[:space:]]*\}\}`)

// withKeys returns a step's `with:` keys in a stable order, so a failure
// message reads the same on every run.
func withKeys(w map[string]any) []string {
	keys := make([]string, 0, len(w))
	for k := range w {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestSetupGoActionsStepOutputReferencesResolveToAnEarlierStep closes the
// second half of #282's blast radius, and the cheaper half.
//
// TestSetupGoActionPinsAVersionRatherThanTheGoModFile requires `go-version` to
// mention `steps.`, and TestSetupGoActionResolvesTheToolchainVersionFromGoMod
// executes the resolver standalone and checks what it writes. Neither joins the
// two: nothing asks whether the id in the reference is the id the resolver
// step actually carries, or whether that step runs FIRST. GitHub resolves an
// unknown or not-yet-run `steps.x.outputs.y` to the empty string rather than
// failing the run, so both breakages are silent by construction, and both are
// one token:
//
//   - rename `id: version` to `id: goversion` and leave the reference alone
//   - move the `uses: actions/setup-go` step above the resolver
//
// Either hands setup-go `go-version: ""`, which drops it back to guessing —
// and guessing is what installs the `go` directive's 1.25.0, downloads
// go1.26.4 into GOMODCACHE from `go env`, and collides with the module-cache
// restore. That is #282 restored in full, reported as a warning rather than an
// error, and I confirmed against this tree that both edits leave every other
// test in this package green.
//
// The property pinned is the general one, because it is the real rule GitHub
// applies and because the next input added here inherits it for free: every
// step-output reference in this action names a step defined by an EARLIER step
// of the same action. It fails closed twice over — a reference it cannot parse
// is an error rather than a skip, judging zero references at all is fatal, and
// `go-version` specifically must be one of the references judged.
func TestSetupGoActionsStepOutputReferencesResolveToAnEarlierStep(t *testing.T) {
	act := readSetupGoAction(t)

	declared := map[string]bool{}
	for _, st := range act.Runs.Steps {
		if st.ID != "" {
			declared[st.ID] = true
		}
	}

	ran := map[string]bool{} // ids of steps that run BEFORE the one being judged
	judged, goVersionRefs := 0, 0
	for _, st := range act.Runs.Steps {
		isSetupGo := stepInstallsGo(st)
		for _, key := range withKeys(st.With) {
			v, isStr := st.With[key].(string)
			if !isStr || !strings.Contains(v, "steps.") {
				continue
			}
			matches := stepOutputRef.FindAllStringSubmatch(v, -1)
			if len(matches) == 0 {
				t.Errorf("%s/action.yml passes `%s: %s`, which reads like a step-output "+
					"reference but is not one GitHub will resolve. It expands to the empty "+
					"string instead, and setup-go given an empty version goes back to guessing "+
					"one (#282).", setupGoActionPath, key, v)
				continue
			}
			for _, m := range matches {
				judged++
				if isSetupGo && key == "go-version" {
					goVersionRefs++
				}
				id := m[1]
				switch {
				case ran[id]:
					// Resolved against a step that has already run. Correct.
				case declared[id]:
					t.Errorf("%s/action.yml passes `%s: %s`, but the step with id %q runs AFTER "+
						"this one. Its outputs do not exist yet, so the reference expands to the "+
						"empty string and setup-go picks a Go version of its own — the collision "+
						"#282 fixed by pinning the toolchain version.", setupGoActionPath, key, v, id)
				default:
					t.Errorf("%s/action.yml passes `%s: %s`, but no step in this action has id %q. "+
						"The reference expands to the empty string, so setup-go picks a Go version "+
						"of its own — the collision #282 fixed by pinning the toolchain version.",
						setupGoActionPath, key, v, id)
				}
			}
		}
		if st.ID != "" {
			ran[st.ID] = true
		}
	}

	if judged == 0 {
		t.Fatalf("%s/action.yml has no step-output reference at all, so this gate judged nothing. "+
			"The Go version reaching setup-go is meant to come from the resolver step (#282).",
			setupGoActionPath)
	}
	if goVersionRefs == 0 {
		t.Errorf("%s/action.yml never hands actions/setup-go a `go-version` that reads a step "+
			"output. The version must come from the resolver step so it cannot drift from "+
			"go.mod's toolchain directive (#282).", setupGoActionPath)
	}
}

var githubOutputRe = regexp.MustCompile(`(?m)^go-version=(.+)$`)

// TestSetupGoActionResolvesTheToolchainVersionFromGoMod EXECUTES the action's
// resolver against this repository's real go.mod. A structural check alone
// would pass a resolver that emits the wrong version, or nothing at all.
func TestSetupGoActionResolvesTheToolchainVersionFromGoMod(t *testing.T) {
	needBinary(t, "bash")
	act := readSetupGoAction(t)
	var script string
	for _, st := range act.Runs.Steps {
		if st.Run != "" {
			if script != "" {
				t.Fatalf("%s/action.yml has more than one `run:` step; this gate cannot tell "+
					"which resolves the version — extend it rather than leaving it guessing", setupGoActionPath)
			}
			script = st.Run
		}
	}
	if strings.TrimSpace(script) == "" {
		t.Fatalf("%s/action.yml has no `run:` step that resolves the Go version", setupGoActionPath)
	}

	outFile := filepath.Join(t.TempDir(), "github_output")
	if err := os.WriteFile(outFile, nil, 0o644); err != nil {
		t.Fatalf("cannot create a stand-in GITHUB_OUTPUT: %v", err)
	}
	cmd := exec.Command("bash", "-c", script)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "GITHUB_OUTPUT="+outFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the resolver in %s/action.yml failed against this repo's go.mod: %v\n%s",
			setupGoActionPath, err, out)
	}
	written, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("cannot read the stand-in GITHUB_OUTPUT: %v", err)
	}
	m := githubOutputRe.FindStringSubmatch(string(written))
	if m == nil {
		t.Fatalf("the resolver in %s/action.yml wrote no `go-version=` to GITHUB_OUTPUT; it wrote:\n%s",
			setupGoActionPath, written)
	}
	if got, want := strings.TrimSpace(m[1]), goModDirective(t); got != want {
		t.Errorf("the resolver emitted go-version=%q; go.mod names %q. setup-go would install "+
			"the wrong toolchain and the module-cache restore would collide (#282)", got, want)
	}
}
