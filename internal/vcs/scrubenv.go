// The scrub policy: which git environment variables formwork removes before it
// runs git, why each one is in the set, and the mechanism that removes them.
//
// It is its own file because it is the one fact this package has to hand to
// callers OUTSIDE it, and because env.go — where it used to live — had grown to
// within a few lines of the 750-line cap that consumers vendoring this source
// enforce.

package vcs

import (
	"slices"
	"sort"
	"strings"
)

// scrubbedGitVars are removed from the environment of every git command this
// package runs. Sorted, because they are named in operator-facing text.
//
// WHY THE FIRST THREE. Each makes git answer about a repository the caller did
// not name — but not by one mechanism, and an earlier version of this paragraph said
// "each overrides the repository git resolves" of all three. GIT_DIR and
// GIT_WORK_TREE do exactly that, beating the `-C root` every call here passes:
// measured on git 2.50.1, `GIT_DIR=B/.git git -C A ls-files` lists B's files.
//
// GIT_COMMON_DIR IS DIFFERENT IN KIND. Measured on 2.50.1 with -C A and
// GIT_COMMON_DIR naming B, the repository git resolves does NOT move — git dir,
// top level, `rev-parse HEAD` and the index are all still A's — while the LOCAL
// CONFIG and the OBJECT STORE become B's: `git -C A config --get k` answered B's
// value for a key A never set, and `git log` failed `bad object HEAD` because
// A's commits are not in B's objects. It redirects what git READS, not which
// repository it resolves, and it is a fail-open all the same — scopedConfig
// (vcs.go) reads local config to decide whose hooks wiring is in force, and
// info/exclude lives in the common directory (see repoIdentity, env.go).
//
// Callers of this package name the repository they mean by argument; an ambient
// variable that silently redirects the answer, by either mechanism, is never an
// intent this package can honour, because the caller has already said which tree
// it is asking about.
//
// THE SET IS TWO FAMILIES, AND THEY ARE SCRUBBED FOR DIFFERENT REASONS — an
// earlier version of this paragraph said "exactly the variables formwork can
// remove without discarding something git or an operator meant", which was
// measurably false, and its successor said the object-store family was out of
// scope. `git rev-parse --local-env-vars` names 15 on 2.50.1; nine are removed
// here, in two groups with two different arguments, and the split is what
// scrubbedRepoVars and scrubbedHistoryVars below record.
//
// The three above are the POINTERS: they move which repository git answers
// about, and because a wrong answer there is indistinguishable from a right one,
// their removal is additionally checked for being inert (gitEnvFor).
//
// THE SECOND FAMILY MOVES THE HISTORY INSIDE THE RIGHT REPOSITORY (#176). git
// resolves the repository the caller named and then answers about a different
// set of commits in it, so no identity comparison can see the difference — the
// git directory, the shared directory and the work tree are all byte-identical.
// The failure is silent and toward passing: a range that resolves to different
// commits reports different findings, and `check --range` exits 0 having judged
// a changeset nobody named. Measured on git 2.50.1, each against
// `diff --name-only HEAD~1..HEAD` in a three-commit repository answering f3.txt:
//
//   - GIT_GRAFT_FILE rewrites commit parentage; with a graft naming HEAD's
//     grandparent as its parent the same range answered `f2.txt f3.txt`.
//   - GIT_REPLACE_REF_BASE points git at another set of replace refs; with a
//     replacement stored under refs/alt-replace/ it answered `f2.txt f3.txt`.
//   - GIT_NO_REPLACE_OBJECTS runs it the other way, SUPPRESSING replace refs the
//     repository itself declares: with a refs/replace/ entry in force the plain
//     answer was `f2.txt f3.txt` and the ambient one f3.txt, so formwork would
//     judge a narrower history than every other git command in that repository.
//   - GIT_OBJECT_DIRECTORY and GIT_SHALLOW_FILE decide whether the range
//     resolves at all — pointed at an empty object directory, and at a shallow
//     boundary cutting the range's base, git answered `fatal: ambiguous
//     argument` where the clean environment answered normally.
//   - GIT_ALTERNATE_OBJECT_DIRECTORIES goes the opposite way, ADDING an object
//     store: a range whose base exists only in ANOTHER repository resolved and
//     returned an empty path set at exit 0, which is this repository's signature
//     defect — a green gate over a changeset from a repository nobody named.
//
// GIT DOES NOT SET ANY OF THE SIX WHERE FORMWORK RUNS, which is what makes
// removing them free. Measured on 2.50.1, the environment git exports into
// pre-commit carries none of them, for a plain commit and for a partial one
// (`git commit -- a.txt`) alike — the same hook site the pointer paragraph
// below dumps.
//
// WHERE IT DOES SET TWO OF THEM IS receive-pack's push QUARANTINE, and that is
// measured rather than recalled: dumping the environment from a bare
// repository's pre-receive hook over a local push, GIT_OBJECT_DIRECTORY named
// `objects/tmp_objdir-incoming-XXXXXX` and GIT_ALTERNATE_OBJECT_DIRECTORIES the
// repository's real `objects`, alongside GIT_QUARANTINE_PATH and the GIT_DIR=.
// the paragraph below records. Scrubbed there, git would not see the incoming
// objects at all — loudly, `bad object`, never a quiet pass. It is not a context
// formwork runs in: formwork installs no server-side hook, which is the same
// argument that GIT_DIR=. measurement already rests on. GIT_SHALLOW_FILE belongs
// to the same server-side flow by git's documentation; no push shape was found
// here that sets it, so that half is documentation rather than measurement.
//
// Two neighbours in --local-env-vars are absent for a stronger reason than
// scope — removing either would BREAK something git or an operator meant:
//
//   - GIT_INDEX_FILE MUST STAY. git points it at a temporary index during a
//     partial commit (`git commit -- one.txt`) and then runs pre-commit, so
//     under --staged it names the index that is about to become the commit.
//     Removing it would make formwork read .git/index instead and judge a file
//     set that is not the one being committed — a scrub here would CREATE a
//     fail-open, not close one. TestGitCallsPreserveGitIndexFile pins it.
//   - GIT_EXEC_PATH MUST STAY. git exports it to its own subprocesses and a
//     non-default installation depends on it to find its helper binaries.
//
// SCRUBBING IS SAFE BECAUSE GIT BARELY SETS THE POINTERS WHERE FORMWORK RUNS.
// Measured on git 2.50.1 (macOS): the environment git exports into pre-commit, dumped
// from formwork's own installed shim, carries GIT_AUTHOR_*, GIT_EDITOR,
// GIT_EXEC_PATH, GIT_INDEX_FILE and GIT_PREFIX — no GIT_DIR; pre-push carries
// none of the three either. The one flow that originates GIT_DIR and can have
// formwork inside it is `submodule foreach`, which sets the relative `.git` and
// runs in the submodule's working tree; scrubbed, git re-discovers the same
// repository through the gitlink and returns byte-identical answers.
// TestSubmoduleForeachEnvironmentSurvivesTheScrub re-measures that rather than
// trusting this paragraph.
//
// IT IS NOT THE ONLY PLACE GIT ORIGINATES GIT_DIR, which an earlier version of
// this paragraph claimed. Measured on 2.50.1: `filter-branch` exports an
// absolute GIT_DIR plus GIT_WORK_TREE=. into every tree-filter, and all four
// receive-pack hooks — pre-receive, update, post-receive, post-update — run with
// GIT_DIR=. (each dumped from a local push). Neither is a context formwork runs
// in — a tree-filter is not a hook site and formwork installs no server-side
// hook — so what survives is the FREQUENCY argument, which is what this
// paragraph rests on, and not the uniqueness one.
// GIT_PREFIX and GIT_IMPLICIT_WORK_TREE complete the 15 and are left alone:
// git sets GIT_PREFIX in every hook and formwork passes an explicit -C, and
// GIT_IMPLICIT_WORK_TREE is git-internal and moved the one answer it was
// measured against (`diff --name-only HEAD~1..HEAD` was byte-identical with it
// set to 1) not at all, which is a narrower statement than "moves nothing". The
// GIT_CONFIG family carries deliberately supplied CONFIGURATION and is refused
// on effect rather than scrubbed (configenv.go, #167 D9): silently discarding an
// operator's `-c` propagation is a different wrong from ignoring an ambient
// pointer, and there is no correct spelling to normalise it into.
var (
	// scrubbedRepoVars move which repository git answers about. Removing one is
	// checked for being inert, because the caller cannot see a moved answer.
	scrubbedRepoVars = []string{"GIT_COMMON_DIR", "GIT_DIR", "GIT_WORK_TREE"}
	// scrubbedHistoryVars move which commits and objects git answers from,
	// INSIDE the repository the caller named (#176). No identity comparison can
	// see them, so there is nothing to arm: removing them is the whole fix.
	scrubbedHistoryVars = []string{
		"GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_GRAFT_FILE",
		"GIT_NO_REPLACE_OBJECTS",
		"GIT_OBJECT_DIRECTORY",
		"GIT_REPLACE_REF_BASE",
		"GIT_SHALLOW_FILE",
	}
	// scrubbedGitVars is the union, sorted, and is what scrubEnviron removes and
	// what operator-facing text names.
	scrubbedGitVars = sortedUnion(scrubbedRepoVars, scrubbedHistoryVars)
)

// sortedUnion concatenates and sorts, so the operator-facing text and the scrub
// cannot disagree about the set with one of them left un-updated.
func sortedUnion(a, b []string) []string {
	out := append(append([]string{}, a...), b...)
	sort.Strings(out)
	return out
}

// THE POLICY ABOVE IS EXPORTED, AND THAT IS THE WHOLE OF #307. A caller that
// cannot reach it restates it, and one did: internal/buildloop's gitScrubbed
// removed GIT_DIR, GIT_WORK_TREE and GIT_COMMON_DIR under the comment "matching
// internal/vcs's policy", which has been nine names since #176. Measured on git
// 2.50.1 against that divergence, `seat-checkout cut <eng> <sha-only-in-oth>
// <dest>` refuses at exit 2 in a clean environment and exits 0, with oth's tree
// checked out, under an ambient GIT_ALTERNATE_OBJECT_DIRECTORIES.
//
// A RESTATEMENT CANNOT BE KEPT IN STEP BY REVIEW — the copy was written five
// hours after #213 landed and was already three names short — so the seam is the
// fix rather than a second correction of the copy. internal/rules/command
// reached the same conclusion from the other side and says so at
// ensureRepositoryAgreement: it calls vcs.EnsureNoInheritedHistoryEnv rather
// than listing the six object-store names, "so this package does not restate a
// list that would drift from the one #176 scrubs".
//
// There is ONE implementation. This package's own git calls go through
// ScrubEnviron too (gitEnv, ensureHatchNamesRootsRepository), so a caller
// outside cannot be given a narrower policy than the one formwork applies to
// itself without the difference showing up in this package's own answers.

// ScrubbedGitVars returns the sorted names ScrubEnviron removes. It is what
// operator-facing text names — gitEnvInheriting's refusal and GitEnvNotice both
// print it — so a caller announcing the policy and a caller applying it cannot
// describe different sets.
//
// The slice is a copy: the policy is not a caller's to edit.
func ScrubbedGitVars() []string { return slices.Clone(scrubbedGitVars) }

// ScrubEnviron splits env into the entries a git command keeps and the sorted
// names of scrubbedGitVars it removed. It is the mechanism of the scrub, with no
// opinion about whether the caller is scrubbing: gitEnv applies it to a real
// run, and ensureHatchNamesRootsRepository (hatch.go) builds the comparison
// environment with it under the hatch, where the real run is not scrubbed at
// all.
//
// The removed names are returned rather than discarded because a caller that
// removed something owes the operator the name of it — empty when none of them
// were set, which is the ordinary case and the one gitEnvFor uses to skip its
// probe entirely.
//
// An entry with no "=" is not a variable this can be about, so it is kept rather
// than parsed further.
//
// Names are compared EXACTLY, which is what the environment is on unix. A
// Windows port would need a case fold here instead: Windows resolves environment
// names case-insensitively, so `Git_Dir` would survive this loop and git would
// honour it anyway.
func ScrubEnviron(env []string) (kept, removed []string) {
	kept = make([]string, 0, len(env))
	for _, kv := range env {
		if name, _, ok := strings.Cut(kv, "="); ok && slices.Contains(scrubbedGitVars, name) {
			removed = append(removed, name)
			continue
		}
		kept = append(kept, kv)
	}
	slices.Sort(removed) // named in operator-facing text
	return kept, removed
}

// MovedRepository keeps the names in removed that belong to the POINTER family —
// the only ones the identity probe can be about.
//
// IT IS WHAT ARMS THAT PROBE, and the distinction is load-bearing rather than
// tidy: a history variable is scrubbed and then nothing more is owed, because
// git answers about the same repository under both environments and the
// comparison would be two identical answers at the cost of two forks. Naming one
// in the probe's refusal would also be false — that message says the variable
// "moves the repository git resolves", and this family does not.
//
// The caller passes the names IT removed rather than asking for the family, so
// the arming decision is about this environment and not about the policy in the
// abstract.
func MovedRepository(removed []string) []string {
	var out []string
	for _, name := range removed {
		if slices.Contains(scrubbedRepoVars, name) {
			out = append(out, name)
		}
	}
	return out
}
