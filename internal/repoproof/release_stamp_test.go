// release_stamp_test.go — the binary release.yml ASSERTS is the binary it
// PUBLISHES, and it asserts one before publishing the other (#281's class, in
// .goreleaser.yaml).
//
// WHY THIS EXISTS. release.yml's publish job builds one target, runs
// cmd/assert-trusted-stamp against it, and then publishes a REBUILD —
// `goreleaser release --clean` wipes that dist/ and builds all four archives
// again. The comment over that step says so, and says what makes it sound:
//
//	ASSUMPTION THIS RESTS ON: the publish step below rebuilds (--clean wipes
//	this dist/), so the asserted bytes are not the published bytes. That is
//	sound only while the stamp is a pure function of (tag, .goreleaser.yaml) —
//	one builds: entry, one shared ldflags line, and a target-independent
//	isReleaseVersion.
//
// Nothing held any of it, which is exactly the shape #281 is: a workflow
// comment asserting a coupling with no mechanism, in a file whose sibling
// comment made the same mistake about branch protection and held it for three
// issues. Reproduced against the real tree, each planted alone, `go test
// ./internal/repoproof/ -count=1` plus `make check` and `make lint` all green
// through every one of them:
//
//   - a second `builds:` entry with no version stamp at all
//   - the pre-publish stamp assertion DELETED from release.yml
//   - the ldflags stamp made target-dependent with `{{ .Os }}`
//
// The first is the one worth spelling out, because it does not merely weaken
// the assertion — it aims it at the wrong file. cmd/assert-trusted-stamp walks
// dist/ and takes the FIRST entry named `formwork` under a path containing
// `linux_amd64`, and GoReleaser gives each build id its own directory. Two
// build ids means two such binaries and a lexical race for which one gets
// asserted, on the PR gate as well as the release.
//
// WHAT THIS FILE HOLDS, and what it does not. The two mechanical halves are
// held here: one build spec, a stamp that cannot vary by target, and an
// assertion that runs before the publish. The third — "a target-independent
// isReleaseVersion" — is a claim about internal/cli/version.go, which this gate
// deliberately does not read: it is a property of Go code, not of
// configuration, and a grep for `runtime.GOOS` would be a rule pretending to be
// a proof. It stays an instruction to a human, and the comment in release.yml
// now says which half is which, so the next reader can tell an enforced claim
// from an aspirational one — the whole lesson of #281.
package repoproof_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// goreleaserConfigPath is the build spec both workflows drive.
const goreleaserConfigPath = ".goreleaser.yaml"

// versionLdflagTarget is the symbol the release stamp is written into. It is
// what cmd/assert-trusted-stamp compares against dist/metadata.json, and the
// 2026-07-23 regression is what happens when the reported version and the
// stamped one drift apart.
const versionLdflagTarget = "github.com/buildfoundry-nz/formwork/internal/cli.version"

// releaseJobID is the job in release.yml that publishes.
const releaseJobID = "release"

// stampAssertionCommand is the one command both workflows run — ci.yml against
// a snapshot on every PR, release.yml against the real tagged build before
// anything is published.
const stampAssertionCommand = "go run ./cmd/assert-trusted-stamp"

// publishCommand is the step that makes a release public. Nothing after it can
// un-publish a binary, which is why the assertion has to come before it.
const publishCommand = "goreleaser release"

// joinLines renders a defect list for a failure message.
func joinLines(defects []string) string { return strings.Join(defects, "\n  ") }

// containsAny reports whether any defect names sub.
func containsAny(defects []string, sub string) bool {
	for _, d := range defects {
		if strings.Contains(d, sub) {
			return true
		}
	}
	return false
}

// goreleaserBuild is one `builds:` entry, decoded down to the fields that
// decide whether the stamp varies by target.
//
// Ldflags is `any` because GoReleaser accepts both a bare string and a list,
// and a typed []string would make the whole file undecodable rather than
// reporting on the shape it actually found.
type goreleaserBuild struct {
	ID        string           `yaml:"id"`
	Main      string           `yaml:"main"`
	Ldflags   any              `yaml:"ldflags"`
	Overrides []map[string]any `yaml:"overrides"`
}

type goreleaserConfig struct {
	Builds []goreleaserBuild `yaml:"builds"`
}

func readGoreleaserConfig(t *testing.T) goreleaserConfig {
	t.Helper()
	path := filepath.Join(repoRoot(t), goreleaserConfigPath)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s — the release build spec both workflows drive: %v",
			goreleaserConfigPath, err)
	}
	var cfg goreleaserConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("cannot parse %s: %v", goreleaserConfigPath, err)
	}
	return cfg
}

// parseGoreleaserConfig decodes a build spec written inline, so this gate can
// be judged against shapes that are not in the tree. Fails closed on a fixture
// it cannot decode: a gate proved against nothing proves nothing.
func parseGoreleaserConfig(t *testing.T, src string) goreleaserConfig {
	t.Helper()
	var cfg goreleaserConfig
	if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatalf("cannot parse the planted %s: %v", goreleaserConfigPath, err)
	}
	return cfg
}

// parseWorkflow decodes a workflow written inline, for the same reason.
func parseWorkflow(t *testing.T, src string) workflow {
	t.Helper()
	var wf workflow
	if err := yaml.Unmarshal([]byte(src), &wf); err != nil {
		t.Fatalf("cannot parse the planted workflow: %v", err)
	}
	return wf
}

// targetIndependentTemplateFields are the GoReleaser template fields that
// resolve to the same text for every goos/goarch in a build. A stamp built
// only from these is the same on the single-target build release.yml asserts
// and on the rebuild it publishes.
//
// The list is an ALLOWLIST, and everything outside it — `.Os`, `.Arch`,
// `.Arm`, `.Target`, `.Ext`, and whatever GoReleaser adds next — is reported.
// Naming the target-dependent fields instead would pass every field this gate
// had not heard of, which is the fail-open the whole file is about.
var targetIndependentTemplateFields = map[string]bool{
	"Version": true, "RawVersion": true, "Tag": true, "Branch": true,
	"Major": true, "Minor": true, "Patch": true, "Prerelease": true,
	"Commit": true, "ShortCommit": true, "FullCommit": true,
	"CommitDate": true, "CommitTimestamp": true,
	"Date": true, "Timestamp": true, "Now": true,
	"ProjectName": true, "ModulePath": true, "ReleaseURL": true,
	"Summary": true, "PrefixedSummary": true,
	"TagSubject": true, "TagContents": true, "TagBody": true,
	"IsGitDirty": true, "IsGitClean": true, "GitTreeState": true,
}

var (
	templateExprRe = regexp.MustCompile(`\{\{([^{}]*)\}\}`)
	simpleFieldRe  = regexp.MustCompile(`^\.([A-Za-z][A-Za-z0-9_]*)$`)
)

// ldflagLines normalises a build's `ldflags`, which GoReleaser accepts as
// either a bare string or a list of them. Any other shape is an error rather
// than an empty list: an ldflags this gate cannot read is an ldflags it cannot
// clear.
func ldflagLines(v any) ([]string, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case string:
		return []string{x}, nil
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("ldflags entry %#v is not a string", e)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("ldflags is %#v, neither a string nor a list of strings", v)
	}
}

// goreleaserStampDefects returns every reason the stamp on the single-target
// build release.yml ASSERTS could differ from the stamp on the four-target
// rebuild it PUBLISHES — one line per defect, empty when there is none.
func goreleaserStampDefects(cfg goreleaserConfig) []string {
	if len(cfg.Builds) == 0 {
		return []string{fmt.Sprintf("%s declares no `builds:` entry, so this gate cannot say "+
			"what a tag publishes or how it is stamped", goreleaserConfigPath)}
	}
	var defects []string
	if len(cfg.Builds) > 1 {
		ids := make([]string, 0, len(cfg.Builds))
		for _, b := range cfg.Builds {
			ids = append(ids, b.ID)
		}
		defects = append(defects, fmt.Sprintf("%s declares %d `builds:` entries (%s). "+
			"cmd/assert-trusted-stamp takes the FIRST binary named `formwork` under a path "+
			"containing `linux_amd64`, and GoReleaser gives each build id a directory of its "+
			"own, so which build gets asserted becomes a lexical accident — on the PR gate as "+
			"much as before a publish",
			goreleaserConfigPath, len(cfg.Builds), strings.Join(ids, ", ")))
	}
	b := cfg.Builds[0]
	if len(b.Overrides) > 0 {
		defects = append(defects, fmt.Sprintf("build %q declares `overrides:`, which is how a "+
			"target gets ldflags of its own. A stamp proved on one target then says nothing "+
			"about the three that are published beside it", b.ID))
	}
	lines, err := ldflagLines(b.Ldflags)
	if err != nil {
		return append(defects, fmt.Sprintf("build %q: %v — this gate cannot say whether the "+
			"stamp varies by target", b.ID, err))
	}
	if len(lines) == 0 {
		return append(defects, fmt.Sprintf("build %q declares no ldflags, so nothing writes "+
			"%s and every published binary reports the `dev` sentinel", b.ID, versionLdflagTarget))
	}
	stamped := false
	for _, line := range lines {
		if strings.Contains(line, versionLdflagTarget+"=") {
			stamped = true
		}
		for _, m := range templateExprRe.FindAllStringSubmatch(line, -1) {
			expr := strings.TrimSpace(m[1])
			field := simpleFieldRe.FindStringSubmatch(expr)
			if field == nil {
				defects = append(defects, fmt.Sprintf("build %q's ldflags carry `{{%s}}`, which "+
					"is not a plain field reference. This gate cannot say whether it resolves "+
					"the same way for every target, and a stamp it cannot classify is one the "+
					"pre-publish assertion cannot stand in for", b.ID, m[1]))
				continue
			}
			if !targetIndependentTemplateFields[field[1]] {
				defects = append(defects, fmt.Sprintf("build %q's ldflags reference `{{ .%s }}`, "+
					"which is not known to resolve the same way for every target. release.yml "+
					"asserts one target and publishes four; a stamp that varies between them "+
					"makes that assertion true of a binary nobody ships", b.ID, field[1]))
			}
		}
	}
	if !stamped {
		defects = append(defects, fmt.Sprintf("no ldflags line on build %q sets %s, so the "+
			"published binary carries the `dev` sentinel and cmd/assert-trusted-stamp is "+
			"comparing against a version nothing stamped", b.ID, versionLdflagTarget))
	}
	return defects
}

// needsList normalises a job's `needs:`, which GitHub accepts as a bare job id
// or a list of them.
func needsList(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// runPosition returns where needle first appears in a job's `run:` scripts, as
// (step index, byte offset within that step). The offset is what makes a
// multi-line `run: |` judgeable: two commands in one step share a step index,
// and only their order inside the script says which runs first.
func runPosition(job wfJob, needle string) (step, off int, found bool) {
	for i, st := range job.Steps {
		if k := strings.Index(st.Run, needle); k >= 0 {
			return i, k, true
		}
	}
	return 0, 0, false
}

// releasePublishDefects returns every reason release.yml could publish a binary
// that was never re-verified or never asserted — one line per defect, empty
// when there is none.
func releasePublishDefects(wf workflow) []string {
	job, ok := wf.Jobs[releaseJobID]
	if !ok {
		return []string{fmt.Sprintf("release.yml declares no %q job, so this gate cannot say "+
			"what publishes a tag or what it proves first", releaseJobID)}
	}
	var defects []string
	if !slices.Contains(needsList(job.Needs), verifyJobID) {
		defects = append(defects, fmt.Sprintf("the %q job does not declare `needs: %s`. A tag "+
			"can be pushed at any SHA and nothing else says it points at a CI-green commit, so "+
			"without that dependency the publish is the only thing a tag runs",
			releaseJobID, verifyJobID))
	}
	assertStep, assertOff, hasAssert := runPosition(job, stampAssertionCommand)
	publishStep, publishOff, hasPublish := runPosition(job, publishCommand)
	if !hasAssert {
		defects = append(defects, fmt.Sprintf("the %q job never runs `%s`. That command is what "+
			"caught the 2026-07-23 regression class, where every released binary was "+
			"stamped-but-untrusted and still passed a naive not-dev check",
			releaseJobID, stampAssertionCommand))
	}
	if !hasPublish {
		defects = append(defects, fmt.Sprintf("the %q job never runs `%s`, so this gate cannot "+
			"say what it publishes or when — and a gate that cannot see the publish cannot "+
			"say anything was proved before it", releaseJobID, publishCommand))
	}
	if hasAssert && hasPublish &&
		(assertStep > publishStep || (assertStep == publishStep && assertOff > publishOff)) {
		defects = append(defects, fmt.Sprintf("the %q job runs `%s` AFTER `%s`. Nothing after "+
			"the publish can un-publish a binary, so an assertion there reports on a release "+
			"that already happened", releaseJobID, stampAssertionCommand, publishCommand))
	}
	return defects
}

// ── fire fixtures: the three plants, and the two shapes too dangerous to
// reproduce in the tree at all ───────────────────────────────────────────────

// oneBuildSpec is what .goreleaser.yaml says today, trimmed to the fields this
// gate reads. It is the control every fire fixture is a single edit away from.
const oneBuildSpec = `
builds:
  - id: formwork
    main: ./cmd/formwork
    ldflags:
      - -s -w -X github.com/buildfoundry-nz/formwork/internal/cli.version={{ .Version }}
`

const plantedSecondBuildSpec = `
builds:
  - id: formwork
    main: ./cmd/formwork
    ldflags:
      - -s -w -X github.com/buildfoundry-nz/formwork/internal/cli.version={{ .Version }}
  - id: formwork-nostamp
    main: ./cmd/formwork
    ldflags:
      - -s -w
`

const plantedTargetDependentStamp = `
builds:
  - id: formwork
    main: ./cmd/formwork
    ldflags:
      - -s -w -X github.com/buildfoundry-nz/formwork/internal/cli.version={{ .Version }}-{{ .Os }}
`

const plantedPerTargetOverride = `
builds:
  - id: formwork
    main: ./cmd/formwork
    ldflags:
      - -s -w -X github.com/buildfoundry-nz/formwork/internal/cli.version={{ .Version }}
    overrides:
      - goos: darwin
        goarch: arm64
        ldflags:
          - -s -w
`

const plantedUnstampedBuild = `
builds:
  - id: formwork
    main: ./cmd/formwork
    ldflags:
      - -s -w
`

// releaseShape renders a release.yml skeleton carrying exactly the fields this
// gate reads, so each fire fixture below differs from the control in one way.
const goodRelease = `
jobs:
  verify:
    steps:
      - run: make verify
  release:
    needs: verify
    steps:
      - name: assert
        run: |
          goreleaser build --clean --single-target
          go run ./cmd/assert-trusted-stamp
      - name: publish
        run: goreleaser release --clean
`

const plantedReleaseWithoutStampAssertion = `
jobs:
  verify:
    steps:
      - run: make verify
  release:
    needs: verify
    steps:
      - name: publish
        run: goreleaser release --clean
`

const plantedReleaseAssertingAfterPublish = `
jobs:
  verify:
    steps:
      - run: make verify
  release:
    needs: verify
    steps:
      - name: publish
        run: goreleaser release --clean
      - name: assert
        run: |
          goreleaser build --clean --single-target
          go run ./cmd/assert-trusted-stamp
`

// The same inversion inside ONE step, which an index comparison alone reads as
// correctly ordered.
const plantedReleaseAssertingAfterPublishInOneStep = `
jobs:
  verify:
    steps:
      - run: make verify
  release:
    needs: verify
    steps:
      - name: publish and assert
        run: |
          goreleaser release --clean
          go run ./cmd/assert-trusted-stamp
`

const plantedReleaseWithoutReVerify = `
jobs:
  verify:
    steps:
      - run: make verify
  release:
    steps:
      - name: assert
        run: |
          goreleaser build --clean --single-target
          go run ./cmd/assert-trusted-stamp
      - name: publish
        run: goreleaser release --clean
`

// TestGoreleaserStampGateCatchesWhatBreaksTheRebuildEquivalence is the fire
// half. Every case is one edit away from the control, and each names the
// substring its defect must carry, so a gate that reported the right NUMBER of
// defects for the wrong reason cannot pass.
func TestGoreleaserStampGateCatchesWhatBreaksTheRebuildEquivalence(t *testing.T) {
	cases := []struct {
		name string
		spec string
		want string
	}{
		{"a second builds entry", plantedSecondBuildSpec, "builds:"},
		{"a target-dependent stamp", plantedTargetDependentStamp, ".Os"},
		{"a per-target override", plantedPerTargetOverride, "overrides:"},
		{"no version stamp at all", plantedUnstampedBuild, versionLdflagTarget},
		{"no builds entry at all", "builds: []\n", "builds:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := goreleaserStampDefects(parseGoreleaserConfig(t, tc.spec))
			if len(got) == 0 {
				t.Fatalf("%s reports no defect in a build spec with %s. release.yml asserts "+
					"the stamp on a single-target build and publishes a rebuild; that is sound "+
					"only while the stamp is a pure function of (tag, %s) (#281).",
					"goreleaserStampDefects", tc.name, goreleaserConfigPath)
			}
			if !containsAny(got, tc.want) {
				t.Errorf("goreleaserStampDefects reported\n  %s\nbut nothing naming %q, so it "+
					"caught this fixture for some other reason", joinLines(got), tc.want)
			}
		})
	}
}

// TestGoreleaserStampGatePassesTheSpecTheRepoShips is the pass half of the
// fixture pair: the control must produce nothing, or every case above is
// meaningless.
func TestGoreleaserStampGatePassesTheSpecTheRepoShips(t *testing.T) {
	if got := goreleaserStampDefects(parseGoreleaserConfig(t, oneBuildSpec)); len(got) != 0 {
		t.Errorf("goreleaserStampDefects condemns the shape .goreleaser.yaml actually ships:\n  %s",
			joinLines(got))
	}
}

// TestReleasePublishGateCatchesAPublishThatWasNeverAsserted is the fire half
// for release.yml. The one-step case is the reason the gate compares positions
// inside a step and not just step indexes: `run: |` puts both commands at the
// same index, and an index comparison calls that correctly ordered.
func TestReleasePublishGateCatchesAPublishThatWasNeverAsserted(t *testing.T) {
	cases := []struct {
		name string
		wf   string
		want string
	}{
		{"the stamp assertion deleted", plantedReleaseWithoutStampAssertion, stampAssertionCommand},
		{"asserted after publishing", plantedReleaseAssertingAfterPublish, "AFTER"},
		{"asserted after publishing in one step", plantedReleaseAssertingAfterPublishInOneStep, "AFTER"},
		{"published without re-verifying", plantedReleaseWithoutReVerify, "needs:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := releasePublishDefects(parseWorkflow(t, tc.wf))
			if len(got) == 0 {
				t.Fatalf("releasePublishDefects reports no defect in a release workflow with %s. "+
					"A binary that would gate as untrusted must never ship (#281).", tc.name)
			}
			if !containsAny(got, tc.want) {
				t.Errorf("releasePublishDefects reported\n  %s\nbut nothing naming %q, so it "+
					"caught this fixture for some other reason", joinLines(got), tc.want)
			}
		})
	}
}

// TestReleasePublishGatePassesTheWorkflowTheRepoShips is that pair's pass half.
func TestReleasePublishGatePassesTheWorkflowTheRepoShips(t *testing.T) {
	if got := releasePublishDefects(parseWorkflow(t, goodRelease)); len(got) != 0 {
		t.Errorf("releasePublishDefects condemns the shape release.yml actually ships:\n  %s",
			joinLines(got))
	}
}

// TestGoreleaserStampIsTargetIndependent applies the gate to the real build
// spec. The fixtures above say the gate can see these defects; this says the
// tree does not have them.
func TestGoreleaserStampIsTargetIndependent(t *testing.T) {
	if got := goreleaserStampDefects(readGoreleaserConfig(t)); len(got) != 0 {
		t.Errorf("%s no longer satisfies what release.yml's publish step assumes:\n  %s\n"+
			"That step asserts the stamp on a `--single-target` build and then publishes a "+
			"`--clean` rebuild, so the asserted bytes are never the published bytes. The "+
			"assertion means something only while the stamp is a pure function of (tag, %s) "+
			"(#281).", goreleaserConfigPath, joinLines(got), goreleaserConfigPath)
	}
}

// TestReleaseAssertsTheStampAndReVerifiesBeforePublishing applies the other
// gate to the real workflow.
func TestReleaseAssertsTheStampAndReVerifiesBeforePublishing(t *testing.T) {
	wf := readWorkflow(t, releaseWorkflowPath(t))
	if got := releasePublishDefects(wf); len(got) != 0 {
		t.Errorf("release.yml can publish a binary it never proved:\n  %s\n"+
			"A tag can be pushed at any SHA, and the 2026-07-23 regression shipped every "+
			"released binary stamped-but-untrusted while passing a naive not-dev check. The "+
			"re-verify and the stamp assertion are what stand between a tag and a release "+
			"(#281).", joinLines(got))
	}
}

// goreleaserArtifactCommands are the goreleaser invocations that produce a
// binary someone could ship. `goreleaser check` is deliberately absent: it
// validates the config and builds nothing.
var goreleaserArtifactCommands = []string{"goreleaser build", "goreleaser release"}

// TestNoJobBuildsAReleaseArtifactWithoutAssertingIt holds the claim both
// workflows make in prose — "one command so the two workflows cannot drift".
//
// cmd/assert-trusted-stamp is shared on purpose: ci.yml runs it against a
// snapshot on every PR, release.yml against the real tagged build before
// anything is published, and the 2026-07-23 regression is what it exists to
// catch — every released binary stamped-but-untrusted, passing a naive not-dev
// check the whole time. Deleting the step from ci.yml's release-config job left
// every test in this repository green, with the comment claiming the sharing
// still sitting above the gap.
//
// The property is stated over jobs rather than over the two files by name, so a
// third workflow that builds an artifact inherits it. It fails closed on a tree
// where no job builds one at all.
//
// It deliberately overlaps releasePublishDefects on release.yml's publish job:
// that gate owns the ORDER and this one owns the PRESENCE, and a job that never
// runs the assertion should be caught by both rather than land in the seam.
func TestNoJobBuildsAReleaseArtifactWithoutAssertingIt(t *testing.T) {
	judged := 0
	for _, path := range workflowPaths(t) {
		wf := readWorkflow(t, path)
		rel, _ := filepath.Rel(repoRoot(t), path)
		for _, id := range jobIDs(wf) {
			job := wf.Jobs[id]
			var builds string
			for _, cmd := range goreleaserArtifactCommands {
				if _, _, ok := runPosition(job, cmd); ok {
					builds = cmd
					break
				}
			}
			if builds == "" {
				continue
			}
			judged++
			if _, _, ok := runPosition(job, stampAssertionCommand); !ok {
				t.Errorf("%s job %q runs `%s` but never `%s`. That command is the one thing "+
					"standing between a stamped-but-untrusted binary and a release, and both "+
					"workflows' comments claim they share it (#281).",
					rel, id, builds, stampAssertionCommand)
			}
		}
	}
	if judged == 0 {
		t.Fatalf("no job in any workflow runs a goreleaser build or release, so this gate "+
			"judged nothing. It is meant to be watching %v", goreleaserArtifactCommands)
	}
}

// releaseStampGuard is this file, spelled the way both workflows have to spell
// it. The tests above make their claims true; this constant is how a reader of
// a workflow finds them.
const releaseStampGuard = "internal/repoproof/release_stamp_test.go"

// TestWorkflowsNameTheGuardOverTheStampAssumptions closes the readable half,
// for the same reason TestReleaseNamesTheGuardThatKeepsItInStepWithCI does. An
// enforced claim and an aspirational one look identical in a comment, and
// release.yml's publish step carries both at once now: two of its three
// assumptions are checked here and the third — a target-independent
// isReleaseVersion in internal/cli/version.go — is not, because it is a
// property of Go code rather than of configuration. A reader has to be able to
// tell which is which, and the only place they are reading is the comment.
func TestWorkflowsNameTheGuardOverTheStampAssumptions(t *testing.T) {
	guard := filepath.Join(repoRoot(t), filepath.FromSlash(releaseStampGuard))
	if _, err := os.Stat(guard); err != nil {
		t.Fatalf("%s: %v — both workflows are told to name this file, so a rename moves this "+
			"constant and their comments with it rather than leaving readers pointed at "+
			"nothing (#281)", releaseStampGuard, err)
	}
	for _, path := range []string{ciWorkflowPath(t), releaseWorkflowPath(t)} {
		rel, _ := filepath.Rel(repoRoot(t), path)
		if !strings.Contains(yamlComments(t, path), releaseStampGuard) {
			t.Errorf("%s claims the stamp assertion is shared and load-bearing but does not say "+
				"what makes that true. Name %s in the comment: an unattributed claim is what "+
				"#281 is.", rel, releaseStampGuard)
		}
	}
}
