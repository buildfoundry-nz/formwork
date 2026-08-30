// ci_contexts_test.go — the check contexts main's ruleset must require are
// exactly the ones ci.yml produces, and ci.yml says so where a maintainer
// reads it (#281).
//
// WHAT HAPPENED. PR #254 was merged at 2026-08-25T21:28:58Z: eight seconds
// after `verify (darwin)` reported FAILURE and ninety-six after `verify
// (linux)` did, with `autoMergeRequest: null` — a deliberate immediate merge
// over two red legs. Both were failing on a real test, not infrastructure. Run
// 32900927171 ran on headSha 34ca26a2, PR #254's own head with nothing pushed
// after it, and both legs printed
// `FAIL github.com/buildfoundry-nz/formwork/internal/publication`. main's own
// push run went red at 21:29:01Z and stayed red until hotfix PR #257's run at
// 22:03:11Z, 34 minutes with no intervening main run. The PR body meanwhile
// asserted "`make verify` exit 0 (redirected, code read directly)".
//
// WHY NOTHING STOPPED IT, and what changed. In the private repository this
// tree was cut from, `gh api repos/<owner>/<repo>/branches/main/protection`
// returned 404 "Branch not protected" and `gh api .../rulesets` returned `[]` —
// asked with an admin-scoped token, so that was the real answer and not a
// permissions artefact. None of the three contexts was required, so CI was
// advisory: it reported, and a merge proceeded regardless. #281 was closed on
// the owner's decision to leave it that way, with the cost written down.
//
// GOING PUBLIC REVERSED THAT DECISION, and the reversal is the reason this
// file's requirements grew a second half. The argument for leaving protection
// off was that a required-checks ruleset makes a flaky darwin leg a hard block
// for a small team who can be trusted to read the checks before merging. A
// public repository takes pull requests from people who are not in that group
// and hold no write access at all, so the mitigation stops applying to the
// population it needs to cover. main now requires a PULL REQUEST APPROVED BY A
// CODE OWNER with all three contexts green.
//
// THAT IS A REPOSITORY SETTING AND NO FILE IN THIS TREE CAN SET IT. Nor can a
// workflow assert it: GITHUB_TOKEN has no `administration` permission scope, so
// a CI job cannot even READ branch protection without a PAT or App token in a
// secret. What the tree holds is the instruction:
//
//	gh api -X POST repos/buildfoundry-nz/formwork/rulesets -f name=main \
//	  -f target=branch -f enforcement=active \
//	  -F 'conditions[ref_name][include][]=~DEFAULT_BRANCH' \
//	  -F 'rules[][type]=pull_request' \
//	  -F 'rules[][parameters][required_approving_review_count]=1' \
//	  -F 'rules[][parameters][require_code_owner_review]=true' \
//	  -F 'rules[][parameters][dismiss_stale_reviews_on_push]=true' \
//	  -F 'rules[][type]=required_status_checks' \
//	  -F 'rules[][parameters][strict_required_status_checks_policy]=true' \
//	  -F 'rules[][parameters][required_status_checks][][context]=verify (linux)' \
//	  -F 'rules[][parameters][required_status_checks][][context]=verify (darwin)' \
//	  -F 'rules[][parameters][required_status_checks][][context]=release-config (validate build)' \
//	  -F 'bypass_actors=[]'
//
// `require_code_owner_review` resolves against .github/CODEOWNERS and is
// satisfied by NOBODY on any path that file does not cover, so the two halves
// are one gate — codeowners_test.go holds the half that is in the tree, and it
// exists because an organization named as an owner (`@org`, not `@org/team`)
// resolves to no owner while reading exactly like one that does.
//
// `allow_auto_merge` is left false or turned on deliberately — while it is
// false, `gh pr merge --auto` degrades to an immediate merge, which is how a
// caller asking to wait ends up not waiting.
//
// WHAT THIS FILE DOES DO. Those three context strings are pinned by NAME, and
// a rename is what silently unpins a ruleset that does exist — the ci.yml
// comment above the `verify` job has claimed since #117 that naming the legs by
// platform keeps "the checks branch protection pins" intact, with nothing
// enforcing it and no protection to pin. This test makes that claim true from
// the tree's side: the required list below is the single authority, ci.yml's
// jobs must expand to exactly it, and ci.yml's own comment must name it, so a
// rename cannot land without the person doing it being told that the ruleset
// moves with it.
package repoproof_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// requiredCheckContexts is the authority: the exact set of check contexts that
// main's ruleset must list as required. Adding a CI job that gates a merge, or
// renaming one, changes this list AND the ruleset — the two move together or
// the gate quietly stops gating.
//
// release.yml is deliberately absent. It runs on `push: tags`, never on a pull
// request, so its contexts can never report on a PR and requiring one would
// hang every merge forever.
var requiredCheckContexts = []string{
	"release-config (validate build)",
	"verify (darwin)",
	"verify (linux)",
}

var matrixRefRe = regexp.MustCompile(`\$\{\{\s*matrix\.([A-Za-z0-9_-]+)\s*\}\}`)

// ciCheckContexts expands a workflow's jobs into the check-context names
// GitHub reports for them: the job's `name` if it has one, else its id, with
// each matrix `include` entry substituted in.
//
// It fails closed on any job shape it cannot expand. A gate that guesses at a
// job it does not understand reports a pass it has not earned.
func ciCheckContexts(t *testing.T, path string) []string {
	t.Helper()
	wf := readWorkflow(t, path)
	var out []string
	for _, id := range jobIDs(wf) {
		job := wf.Jobs[id]
		label := job.Name
		if label == "" {
			label = id
		}
		includes := job.Strategy.Matrix.Include
		refs := matrixRefRe.FindAllStringSubmatch(label, -1)
		switch {
		case len(includes) == 0 && len(refs) == 0:
			out = append(out, label)
		case len(includes) == 0 && len(refs) > 0:
			t.Fatalf("%s job %q names matrix values but declares no matrix include entries; "+
				"this gate cannot expand its contexts", path, id)
		case len(refs) == 0:
			t.Fatalf("%s job %q is a matrix job whose name %q carries no matrix value, so its "+
				"legs share one context name; this gate cannot expand it", path, id, label)
		default:
			for _, inc := range includes {
				name := label
				for _, ref := range refs {
					v, ok := inc[ref[1]]
					if !ok {
						t.Fatalf("%s job %q names matrix.%s, which its include entry %v does not set",
							path, id, ref[1], inc)
					}
					s, ok := v.(string)
					if !ok {
						t.Fatalf("%s job %q: matrix.%s is %#v, not a string; this gate cannot expand it",
							path, id, ref[1], v)
					}
					name = strings.ReplaceAll(name, ref[0], s)
				}
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

func ciWorkflowPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), ".github", "workflows", "ci.yml")
}

// yamlComments returns the workflow's comment lines, joined, so a test can ask
// what the file tells the person reading it. Only a line whose first
// non-blank character is `#` counts: a `#` inside a value is not a comment, and
// a gate that read YAML values as prose could be satisfied by a job id.
func yamlComments(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	var comments strings.Builder
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			comments.WriteString(trimmed)
			comments.WriteByte('\n')
		}
	}
	return comments.String()
}

// TestCIProducesExactlyTheCheckContextsMainMustRequire pins the names. A
// runner move must not change them — that is what naming the legs by platform
// buys — and a rename must not slip past unnoticed, because a ruleset requiring
// a context no job reports blocks every merge, and one requiring a context that
// no longer exists blocks nothing at all.
func TestCIProducesExactlyTheCheckContextsMainMustRequire(t *testing.T) {
	got := ciCheckContexts(t, ciWorkflowPath(t))
	want := append([]string(nil), requiredCheckContexts...)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ci.yml produces check contexts\n  %s\nbut main's ruleset must require\n  %s\n"+
			"Whichever is wrong, the ruleset and this list move together — a required context no "+
			"job reports blocks every merge, and a job no context requires gates nothing (#281).",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// TestCIRunsOnPullRequestsSoTheRequiredContextsCanReport is the other half of
// the pin: a required context that never reports on a pull request is not a
// gate, it is a permanent block.
func TestCIRunsOnPullRequestsSoTheRequiredContextsCanReport(t *testing.T) {
	wf := readWorkflow(t, ciWorkflowPath(t))
	if _, ok := wf.On["pull_request"]; !ok {
		t.Errorf("ci.yml does not trigger on pull_request, so none of %v can report on a PR (#281)",
			requiredCheckContexts)
	}
	push, ok := wf.On["push"]
	if !ok {
		t.Fatalf("ci.yml does not trigger on push, so a merge into main is never verified (#281)")
	}
	// Rendered rather than shape-matched on purpose: `branches: [main]` is
	// today's spelling, and any equivalent rewrite that still names main should
	// satisfy this without a rewrite here.
	if !strings.Contains(fmt.Sprintf("%v", push), "main") {
		t.Errorf("ci.yml's push trigger does not name main (%v), so nothing reports on the merged "+
			"result (#281)", push)
	}
}

// rulesetContextRe captures the context named by one `required_status_checks`
// entry of the ruleset-creating command ci.yml's comment carries. The key is
// GitHub's API field name rather than a spelling of ours, so it survives the
// command being reformatted.
//
// It reads the JSON body form. The comment used to carry `gh api -f/-F` flags
// and this pattern matched `[context]=`; both changed together when the -F
// command turned out to be one GitHub rejects with a 422 — `gh` appends a new
// array element at every `rules[]`, so two rules across seven flag lines
// arrive as seven objects matching no schema. A command that cannot run is not
// a weaker instruction than a working one, it is prose, and every test in this
// file passed over it.
var rulesetContextRe = regexp.MustCompile(`"context"\s*:\s*"([^"]+)"`)

// rulesetCommandDefects returns every reason the `gh api` command in a
// workflow's comments would create a ruleset that does not match
// requiredCheckContexts — one line per defect, empty when there is none.
//
// It reports the two directions separately because they fail differently. A
// context the command requires that no job reports blocks every merge into main
// until someone with admin edits the ruleset; a required context the command
// omits leaves that job advisory, which is the state #281 was filed against.
func rulesetCommandDefects(comments string) []string {
	matches := rulesetContextRe.FindAllStringSubmatch(comments, -1)
	if len(matches) == 0 {
		return []string{"the comment carries no `gh api` ruleset command naming any " +
			"`[context]=`. That command is the only actionable instruction for turning the " +
			"merge gate on, and a comment that has lost it leaves the whole of #281 as prose"}
	}
	inCommand := map[string]bool{}
	var named []string
	for _, m := range matches {
		ctx := strings.TrimSpace(m[1])
		if !inCommand[ctx] {
			inCommand[ctx] = true
			named = append(named, ctx)
		}
	}
	required := map[string]bool{}
	for _, ctx := range requiredCheckContexts {
		required[ctx] = true
	}
	var defects []string
	for _, ctx := range requiredCheckContexts {
		if !inCommand[ctx] {
			defects = append(defects, fmt.Sprintf("the ruleset command does not require %q, so "+
				"running it as written leaves that job advisory — it reports and a merge "+
				"proceeds regardless, which is the state #281 was filed against", ctx))
		}
	}
	sort.Strings(named)
	for _, ctx := range named {
		if !required[ctx] {
			defects = append(defects, fmt.Sprintf("the ruleset command requires %q, which no job "+
				"in ci.yml reports. A required context nothing reports never goes green, so "+
				"running it as written blocks every merge into main until someone with admin "+
				"edits the ruleset back", ctx))
		}
	}
	return defects
}

// ── fixtures for the ruleset command, in the shape yamlComments hands over ──

// currentRulesetCommand is what ci.yml's comment carries today, and the control
// each fire fixture below is one edit away from.
//
// In the JSON body form, because the -F form it used to be in is one GitHub
// answers with a 422 — see rulesetContextRe. These fixtures were in that
// spelling too, so the gate was exercised entirely against a command shape
// that cannot create a ruleset.
const currentRulesetCommand = `
# gh api -X POST repos/buildfoundry-nz/formwork/rulesets --input - <<'JSON'
#   "required_status_checks": [
#     { "context": "verify (linux)" },
#     { "context": "verify (darwin)" },
#     { "context": "release-config (validate build)" }
#   ]
# JSON
`

// staleRulesetCommand is the whole point: a leg renamed in the jobs and in the
// prose list, and the command left as it was. Every other test in this file is
// satisfied by that state.
const staleRulesetCommand = `
# gh api -X POST repos/buildfoundry-nz/formwork/rulesets --input - <<'JSON'
#   "required_status_checks": [
#     { "context": "verify (ubuntu)" },
#     { "context": "verify (darwin)" },
#     { "context": "release-config (validate build)" }
#   ]
# JSON
`

const shortRulesetCommand = `
# gh api -X POST repos/buildfoundry-nz/formwork/rulesets --input - <<'JSON'
#   "required_status_checks": [
#     { "context": "verify (linux)" },
#     { "context": "verify (darwin)" }
#   ]
# JSON
`

const overlongRulesetCommand = `
# gh api -X POST repos/buildfoundry-nz/formwork/rulesets --input - <<'JSON'
#   "required_status_checks": [
#     { "context": "verify (linux)" },
#     { "context": "verify (darwin)" },
#     { "context": "release-config (validate build)" },
#     { "context": "lint (linux)" }
#   ]
# JSON
`

// TestRulesetCommandGateCatchesACommandThatHasDriftedFromTheJobs is the fire
// half. Each case names the substring its defect must carry, so a gate that
// reported a defect for the wrong reason cannot pass.
func TestRulesetCommandGateCatchesACommandThatHasDriftedFromTheJobs(t *testing.T) {
	cases := []struct {
		name     string
		comments string
		want     string
	}{
		{"a renamed leg left stale in the command", staleRulesetCommand, "verify (ubuntu)"},
		{"a required context the command never names", shortRulesetCommand, "release-config (validate build)"},
		{"a context the command requires that no job reports", overlongRulesetCommand, "lint (linux)"},
		{"no command at all", "# nothing here names a ruleset\n", "no `gh api`"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rulesetCommandDefects(tc.comments)
			if len(got) == 0 {
				t.Fatalf("rulesetCommandDefects reports no defect for %s. That command is what a "+
					"maintainer with admin actually runs, and a ruleset requiring a context no job "+
					"reports blocks every merge on main permanently (#281).", tc.name)
			}
			if !containsAny(got, tc.want) {
				t.Errorf("rulesetCommandDefects reported\n  %s\nbut nothing naming %q, so it "+
					"caught this fixture for some other reason", joinLines(got), tc.want)
			}
		})
	}
}

// TestRulesetCommandGatePassesTheCommandCIShips is that pair's pass half.
func TestRulesetCommandGatePassesTheCommandCIShips(t *testing.T) {
	if got := rulesetCommandDefects(currentRulesetCommand); len(got) != 0 {
		t.Errorf("rulesetCommandDefects condemns the command ci.yml actually carries:\n  %s",
			joinLines(got))
	}
}

// TestCIsRulesetCommandRequiresExactlyTheContextsItsJobsReport applies the gate
// to the real workflow. requiredCheckContexts is the authority for both the
// jobs and this command; the two copies of it in one comment are what makes a
// silent divergence possible.
func TestCIsRulesetCommandRequiresExactlyTheContextsItsJobsReport(t *testing.T) {
	if got := rulesetCommandDefects(yamlComments(t, ciWorkflowPath(t))); len(got) != 0 {
		t.Errorf("the ruleset command in ci.yml's comment no longer matches the contexts its "+
			"jobs report:\n  %s\nThat command is copy-pasted by a human holding admin. One "+
			"naming a context no job reports turns main into a branch nothing can merge into, "+
			"and one missing a context leaves that job advisory — which is #281 (#281).",
			joinLines(got))
	}
}

// codeOwnerRulesetRequirements are the parameters that make main's ruleset a
// REVIEW gate rather than only a checks gate. Each is a separate way for the
// command to be copy-pasted into a weaker ruleset than the comment around it
// describes, and each fails silently: the call returns 201, `gh api rulesets`
// answers with a ruleset, and the thing everyone believes is on is half on.
//
// Spelled as the API's own field names, so a reformatting of the command does
// not slip past and a reader can check each one against GitHub's reference.
var codeOwnerRulesetRequirements = []struct{ token, why string }{
	{`"type": "pull_request"`,
		"without a pull_request rule main takes direct pushes, and no review requirement " +
			"of any kind applies to them"},
	{`"require_code_owner_review": true`,
		"without it any approval satisfies the gate, including one from a first-time " +
			"contributor on a public repository — which is the audience change that " +
			"reversed the decision in the first place"},
	{`"required_approving_review_count": 1`,
		"a count of zero leaves require_code_owner_review with nothing to require: the " +
			"pull request needs no approval at all, so no approval is ever checked for " +
			"code ownership"},
	{`"bypass_actors": []`,
		"a bypass list holding the admin who created the ruleset reproduces the #254 " +
			"merge exactly, with a ruleset in place to point at"},
}

// rulesetCommandOnly narrows a comment block to the `gh api` invocation inside
// it — the `gh api` line and its heredoc body, up to the closing delimiter,
// and nothing else.
//
// THE NARROWING IS THE POINT, and it was measured. Asked of the whole comment,
// this gate is satisfied by the PROSE that explains the command: deleting the
// `bypass_actors=[]` flag from the invocation left the sentence "bypass_actors
// is not decoration" standing three paragraphs below, the token was still
// present, and the arm stayed green over a command that no longer carried it.
// A gate a file can satisfy by describing what it no longer does is the exact
// shape this package exists to keep out.
func rulesetCommandOnly(comments string) string {
	var b strings.Builder
	in := false
	for _, line := range strings.Split(comments, "\n") {
		l := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
		switch {
		case strings.HasPrefix(l, "gh api "):
			in = true
		case in && l == "JSON":
			in = false
			continue
		}
		if in {
			b.WriteString(l)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// TestCIsRulesetCommandRequiresACodeOwnerReviewedPullRequest holds the half of
// the command that requiredCheckContexts says nothing about. The contexts arms
// above would pass over a ruleset that requires all three checks and lets
// anyone push straight to main with them green.
func TestCIsRulesetCommandRequiresACodeOwnerReviewedPullRequest(t *testing.T) {
	text := rulesetCommandOnly(yamlComments(t, ciWorkflowPath(t)))
	if text == "" {
		t.Fatal("ci.yml's comments carry no `gh api` invocation, so this arm read an empty\ncommand and would report every requirement satisfied over any file at all")
	}
	for _, req := range codeOwnerRulesetRequirements {
		if !strings.Contains(text, req.token) {
			t.Errorf("ci.yml's ruleset command does not carry %s — %s.\nThat command is what a human holding admin copy-pastes; it is the only place the\nreview half of the merge gate is written down.",
				req.token, req.why)
		}
	}
}

// TestCINamesTheContextsMainsRulesetMustRequire keeps the instruction and the
// jobs together. ci.yml is where a maintainer looks to find out what the merge
// gate is; before #281 it asserted a branch protection that did not exist. If
// the contexts change, the sentence a human acts on changes with them.
func TestCINamesTheContextsMainsRulesetMustRequire(t *testing.T) {
	text := yamlComments(t, ciWorkflowPath(t))
	var missing []string
	for _, ctx := range requiredCheckContexts {
		if !strings.Contains(text, ctx) {
			missing = append(missing, ctx)
		}
	}
	if len(missing) > 0 {
		t.Errorf("ci.yml's comments do not name %d of the check contexts main's ruleset must "+
			"require:\n  %s\nThat sentence is the only place a maintainer is told what to "+
			"configure, and it is what was false before #281 — it claimed a branch protection "+
			"that does not exist.", len(missing), strings.Join(missing, "\n  "))
	}
}

// ── THE SAME SHAPE, IN THE OTHER WORKFLOW ───────────────────────────────────
//
// release.yml's `verify` job re-runs `make verify` before a tag is published,
// because nothing else says a tag points at a CI-green commit. The comment over
// it claims the coupling that makes that meaningful — "on the SAME two-platform
// matrix that defines CI-green in ci.yml ... If ci.yml's matrix changes, this
// one changes with it — including the runner labels, which are part of what
// 'the same matrix' means (#183)" — and, exactly like the branch-protection
// claim above it, nothing enforced it.
//
// The two include lists are byte-identical today, which is what makes the claim
// dangerous rather than harmless: it reads as true, so a reviewer has no reason
// to check it, and the failure it invites is a runner move applied to one file.
// ci.yml moves, release.yml does not, and the darwin tarballs GoReleaser
// publishes are then verified on a machine the merge gate never used — with the
// comment still saying they were the same. release.yml cannot report a check
// context on a pull request, so it is deliberately absent from
// requiredCheckContexts above and the tests below are the only thing holding
// the two files together.

// verifyJobID is the job id both workflows use for the two-platform
// `make verify` leg.
const verifyJobID = "verify"

func releaseWorkflowPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), ".github", "workflows", "release.yml")
}

// verifyJob returns a workflow's verification job, failing closed when the
// workflow has none under that id. A rename is a change to the coupling these
// tests hold, so it has to be made here too rather than silently emptying them.
func verifyJob(t *testing.T, path string) wfJob {
	t.Helper()
	wf := readWorkflow(t, path)
	job, ok := wf.Jobs[verifyJobID]
	if !ok {
		t.Fatalf("%s declares no %q job, so this gate cannot compare what ci.yml verifies "+
			"against what release.yml re-verifies before publishing (#281)", path, verifyJobID)
	}
	return job
}

// sortedKeys orders a `with:`/include map's keys so a rendered comparison reads
// the same on every run whatever order the YAML decoder handed them back in.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// matrixRunnerKey returns the matrix key a job takes its machine from, and
// fails closed on every other shape of `runs-on`. This is what stops the
// comparison below being decorative: two identical include lists say nothing
// about where the jobs run unless `runs-on` is one of the values in them, and a
// job pinned to a literal label would carry its `runner:` entries for show.
func matrixRunnerKey(t *testing.T, path string, job wfJob) string {
	t.Helper()
	s, ok := job.RunsOn.(string)
	if !ok {
		t.Fatalf("%s job %q: runs-on is %#v, not a string, so this gate cannot tell which "+
			"machine the job selects (#281)", path, verifyJobID, job.RunsOn)
	}
	refs := matrixRefRe.FindAllStringSubmatch(s, -1)
	if len(refs) != 1 || strings.TrimSpace(matrixRefRe.ReplaceAllString(s, "")) != "" {
		t.Fatalf("%s job %q runs on %q, which is not a single matrix value; while it is not, "+
			"the include entries this gate compares do not decide the machine it runs on (#281)",
			path, verifyJobID, s)
	}
	return refs[0][1]
}

// matrixLegs renders a job's matrix include entries in declaration order, one
// canonical line per leg, and fails closed on a matrix that declares nothing or
// a leg that names no runner.
func matrixLegs(t *testing.T, path string, job wfJob, runnerKey string) []string {
	t.Helper()
	includes := job.Strategy.Matrix.Include
	if len(includes) == 0 {
		t.Fatalf("%s job %q declares no matrix include entries; a verification that runs on "+
			"no platform is not a verification (#281)", path, verifyJobID)
	}
	out := make([]string, 0, len(includes))
	for _, inc := range includes {
		if _, ok := inc[runnerKey]; !ok {
			t.Fatalf("%s job %q: include entry %v sets no %q, so that leg's runs-on resolves to "+
				"nothing and this gate cannot say which machine verifies it (#281)",
				path, verifyJobID, inc, runnerKey)
		}
		parts := make([]string, 0, len(inc))
		for _, k := range sortedKeys(inc) {
			parts = append(parts, fmt.Sprintf("%s=%v", k, inc[k]))
		}
		out = append(out, strings.Join(parts, " "))
	}
	return out
}

// jobSteps renders the executable content of a job's steps in order: the action
// each one `uses`, the `with:` inputs it passes, and the shell each one `run`s.
// `name:` is deliberately dropped — it is a label on the run page and changes
// nothing about what executes, so a reworded step heading must not read as a
// divergence.
func jobSteps(t *testing.T, path string, job wfJob) []string {
	t.Helper()
	if len(job.Steps) == 0 {
		t.Fatalf("%s job %q declares no steps, so it verifies nothing (#281)", path, verifyJobID)
	}
	out := make([]string, 0, len(job.Steps))
	for i, s := range job.Steps {
		var parts []string
		if s.Uses != "" {
			parts = append(parts, "uses="+s.Uses)
		}
		for _, k := range sortedKeys(s.With) {
			parts = append(parts, fmt.Sprintf("with.%s=%v", k, s.With[k]))
		}
		if run := strings.TrimSpace(s.Run); run != "" {
			parts = append(parts, "run="+run)
		}
		if len(parts) == 0 {
			t.Fatalf("%s job %q step %d neither uses an action nor runs a command; this gate "+
				"cannot say what it does (#281)", path, verifyJobID, i+1)
		}
		out = append(out, strings.Join(parts, " "))
	}
	return out
}

// TestReleaseVerifiesOnTheSameMatrixAsCI makes release.yml's coupling claim
// true. Both legs are compared whole — platform AND runner label — because the
// runner is the half the comment calls out and the half a reader is most likely
// to think of as an implementation detail of one file.
func TestReleaseVerifiesOnTheSameMatrixAsCI(t *testing.T) {
	ciPath, releasePath := ciWorkflowPath(t), releaseWorkflowPath(t)
	ciJob, releaseJob := verifyJob(t, ciPath), verifyJob(t, releasePath)

	ciKey := matrixRunnerKey(t, ciPath, ciJob)
	releaseKey := matrixRunnerKey(t, releasePath, releaseJob)
	if ciKey != releaseKey {
		t.Fatalf("ci.yml's verify job runs on matrix.%s and release.yml's on matrix.%s; the two "+
			"legs cannot be compared entry-for-entry until they select their machine the same "+
			"way (#281)", ciKey, releaseKey)
	}

	ciLegs := matrixLegs(t, ciPath, ciJob, ciKey)
	releaseLegs := matrixLegs(t, releasePath, releaseJob, releaseKey)
	if strings.Join(ciLegs, "\n") != strings.Join(releaseLegs, "\n") {
		t.Errorf("ci.yml's verify matrix is\n  %s\nand release.yml's is\n  %s\n"+
			"release.yml re-verifies a tag on what ci.yml calls green, so the two move together. "+
			"While they differ, a platform's tarballs are published on the strength of a run on a "+
			"machine the merge gate never used (#183, #281).",
			strings.Join(ciLegs, "\n  "), strings.Join(releaseLegs, "\n  "))
	}
}

// TestReleaseRunsTheSameVerificationAsCI is the other half of "the same
// matrix": the same machines running a weaker command is not the same
// verification. A release leg that ran `make test` where ci.yml runs `make
// verify` would publish behind a gate the merge never applied, and the matrix
// comparison above would be satisfied throughout.
func TestReleaseRunsTheSameVerificationAsCI(t *testing.T) {
	ciPath, releasePath := ciWorkflowPath(t), releaseWorkflowPath(t)
	ciSteps := jobSteps(t, ciPath, verifyJob(t, ciPath))
	releaseSteps := jobSteps(t, releasePath, verifyJob(t, releasePath))
	if strings.Join(ciSteps, "\n") != strings.Join(releaseSteps, "\n") {
		t.Errorf("ci.yml's verify job runs\n  %s\nand release.yml's runs\n  %s\n"+
			"A tag is published on the strength of the second, and CI-green means the first. "+
			"If a step must genuinely differ, change this test deliberately and say why (#281).",
			strings.Join(ciSteps, "\n  "), strings.Join(releaseSteps, "\n  "))
	}
}

// releaseCouplingGuard is this file, spelled the way release.yml has to spell
// it. The tests above make its claim true; this constant is how a reader of
// the workflow finds them.
const releaseCouplingGuard = "internal/repoproof/ci_contexts_test.go"

// TestReleaseNamesTheGuardThatKeepsItInStepWithCI closes the readable half. An
// enforced claim and an aspirational one look identical in a comment, and this
// repository has now shipped both: ci.yml asserted a branch protection that did
// not exist from #117 until #281, and release.yml asserts a matrix coupling
// that nothing held until the tests above. The person editing one matrix is the
// person who needs to know where the other half is checked, and the comment is
// the only thing they are reading.
func TestReleaseNamesTheGuardThatKeepsItInStepWithCI(t *testing.T) {
	guard := filepath.Join(repoRoot(t), filepath.FromSlash(releaseCouplingGuard))
	if _, err := os.Stat(guard); err != nil {
		t.Fatalf("%s: %v — release.yml is told to name this file, so a rename moves this "+
			"constant and the comment with it rather than leaving readers pointed at nothing (#281)",
			releaseCouplingGuard, err)
	}
	if text := yamlComments(t, releaseWorkflowPath(t)); !strings.Contains(text, releaseCouplingGuard) {
		t.Errorf("release.yml claims its verify matrix moves with ci.yml's but does not say what "+
			"makes that true. Name %s in the comment: an unattributed claim is what #281 is, and "+
			"a reader cannot tell one that is enforced from one that is merely hoped for.",
			releaseCouplingGuard)
	}
}
