// Hook wiring that is not formwork's. Both sides of #146 need to agree about
// what counts as a real hook file — Verify reports the hooks core.hooksPath is
// shadowing, and Install refuses to create it — and two implementations that
// drift is precisely how a repository loses a hook silently. One set of rules,
// put here so the second side cites it rather than growing its own copy: what a
// real hook file is, where git's default hooks directory is, and whether the two
// paths in question are the same directory.
//
// preexistingHooks is that second side's question, and Install now asks it:
// Install → preflight → runningHooksRefusal. PreexistingHooksForTest in
// export_test.go is still the seam that reaches the detector directly, for the
// reason given at its own site.
package hooks

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// xOK is access(2)'s X_OK. The syscall package does not export it, and pulling
// golang.org/x/sys/unix for one constant would promote an indirect dependency.
const xOK = 0x1

// access is syscall.Access, indirected so a test can present the errno the
// kernel will not produce on demand.
//
// The seam is here for the same reason vcs.SetSameFileForTest is: the arm that
// must be pinned is the one that cannot be reached from outside. EACCES is
// constructible with a chmod, and every OTHER errno is the state where formwork
// could not perform the test at all — which is exactly the state a fail-open
// would certify. Without the seam that arm ships unproven: measured with it
// returning `true, nil`, three tests in this package go red —
// TestVerifyDoesNotCertifyAShimItCouldNotTestForExecutability,
// TestInstallRepairsWhenItCannotTellWhetherTheShimIsExecutable and
// TestPreexistingHooksIsAnErrorWhenAHookFileCannotBeStatted — and each of them
// constructs its errno through this seam, because no chmod can.
var access = syscall.Access

// executableByCaller reports whether the user running formwork can execute
// path — the question git asks, rather than the one the mode bits answer.
//
// `fi.Mode()&0o111 != 0` is "any execute bit anywhere", and against the file's
// OWNER that is a fail-open: access(X_OK) consults the owner bit alone once the
// caller owns the file, so a shim at 0655 (rw-r-xr-x) cannot be run by the
// person committing while two execute bits sit in the mode. Measured before this
// fix, on a shim chmod'ed to 0655: Verify returned no problems at all, which
// cli.go's hooks verify reports as "hooks wired" at exit 0 — over a gate git
// declines to run.
//
// ASKING THE KERNEL IS THE ONLY WAY TO GET GIT'S ANSWER. Computing it would need
// the caller's uid, every group it is in, and on macOS the file's ACL, which no
// arithmetic over Mode() can see.
//
// Only EACCES is a "no". Every other errno is returned, so a caller that could
// not perform the test reports that rather than certifying a file it never
// tested — the same rule realHooksIn applies to a read failure.
//
// syscall.Access is unix-only; the release builds are linux and darwin
// (.goreleaser.yaml), and there is deliberately no build tag, so a port to a
// platform without it fails to compile rather than falling back to mode bits.
func executableByCaller(path string) (bool, error) {
	switch err := access(path, xOK); {
	case err == nil:
		return true, nil
	case errors.Is(err, syscall.EACCES):
		return false, nil
	default:
		return false, err
	}
}

// gitClientHookNames is every name git will execute as a hook, from githooks(5)
// on git 2.50.1 (Apple Git-155).
//
// IT IS NOT wiredHookNames. That set is the three hooks formwork wires a lane
// to; this one answers a different question — "would git ever execute a file
// with this name" — and is used to decide whether a file in a hooks directory
// is protection or inert. Reaching for the wrong one silently narrows either
// what formwork wires or what it is willing to overwrite.
//
// Server-side and `p4-*` names are included deliberately: the question is about
// git, not about relevance to formwork, and over-inclusion errs toward treating
// a file as someone's hook rather than as spare bytes.
//
// This is a version-dependent fact rather than a constant, which is why
// TestGitClientHookNamesCoversEverySampleGitShips cross-checks it against the
// samples a live `git init` writes: every one of those 14 names appears here,
// and a git that grows a hook formwork has not heard of is the case where a
// missing name makes formwork treat real protection as inert.
var gitClientHookNames = map[string]bool{
	"applypatch-msg":        true,
	"commit-msg":            true,
	"fsmonitor-watchman":    true,
	"p4-changelist":         true,
	"p4-post-changelist":    true,
	"p4-pre-submit":         true,
	"p4-prepare-changelist": true,
	"post-applypatch":       true,
	"post-checkout":         true,
	"post-commit":           true,
	"post-index-change":     true,
	"post-merge":            true,
	"post-receive":          true,
	"post-rewrite":          true,
	"post-update":           true,
	"pre-applypatch":        true,
	"pre-auto-gc":           true,
	"pre-commit":            true,
	"pre-merge-commit":      true,
	"pre-push":              true,
	"pre-rebase":            true,
	"pre-receive":           true,
	"prepare-commit-msg":    true,
	"proc-receive":          true,
	"push-to-checkout":      true,
	"reference-transaction": true,
	"sendemail-validate":    true,
	"update":                true,
}

// realHookFile reports whether dir/name is a file git would actually run.
//
// Three axes, and dropping any one of them is a defect in a different
// direction. The NAME must be one git executes — without that axis every
// healthy repository looks like it is hiding the 14 executable `*.sample` files
// git ships, and `*.sample` is not the discriminator either, because
// init.templateDir replaces the samples while `lib.sh` and `pre-commit.bak`
// stay behind. The file must be EXECUTABLE BY THE CALLER — git prints a hint
// and runs nothing otherwise, so a pre-commit the committing user cannot run is
// not protection and reporting it as such is a false positive nobody can clear.
// And it must be a REGULAR FILE OR A SYMLINK to one — a `pre-commit.d/`
// directory is not a hook.
//
// A dangling symlink is not a real hook: git cannot execute it either. The
// executable test then runs on the LINK path, which is what git executes.
func realHookFile(dir, name string) (bool, error) {
	if !gitClientHookNames[name] {
		return false, nil
	}
	p := filepath.Join(dir, name)
	fi, err := os.Lstat(p)
	if err != nil {
		return false, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		if fi, err = os.Stat(p); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return false, nil
			}
			return false, err
		}
	}
	if !fi.Mode().IsRegular() {
		return false, nil
	}
	return executableByCaller(p)
}

// realHooksIn returns, sorted, the names in dir that git would run as hooks.
//
// A read failure is returned, never folded into an empty list: "this directory
// holds no hooks" and "formwork could not find out" are different answers, and
// the caller is deciding whether an operator's gate has been disabled.
func realHooksIn(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		ok, err := realHookFile(dir, e.Name())
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// defaultHooksDir returns the hooks directory git falls back to when
// core.hooksPath is unset: `hooks` under the git directory every worktree of
// this repository SHARES.
//
// The sharing is the whole point, and it is why vcs.CommonDir is asked rather
// than any per-worktree git directory (R5). From a linked worktree the
// per-worktree directory has no `hooks/` of its own, so a caller that reads it
// concludes the repository has no hooks — wrong in the dangerous direction when
// the decision being made is whether someone else's gate is about to be
// switched off.
//
// One place, because both users of this file need the same answer: the detector
// below, deciding whether to refuse, and verify.go's shadowed-hooks report,
// naming what a refusal came too late for.
func defaultHooksDir(root string) (string, error) {
	common, err := vcs.CommonDir(root)
	if err != nil {
		return "", err
	}
	return filepath.Join(common, "hooks"), nil
}

// preexistingHooks returns the hook files git is running from its default hooks
// directory right now, and that directory. It is the question install's refusal
// is made of: is there a hook wiring here that formwork must not take over?
//
// hooksPath is the directory git will actually run hooks from. It is a
// parameter rather than a second `rev-parse` because the callers this file
// serves already hold that answer, and asking twice invites the two calls to
// disagree about the state a single decision is being made against.
//
// NO NAMES IS "NOTHING TO REFUSE OVER", WHICH IS NOT "THE DIRECTORY IS EMPTY".
// Two states produce it. The directory may hold no file git would run, judged
// by realHookFile above — its three axes are what keep a `README`, a
// `pre-commit` nobody can execute, and the samples git ships out of the answer.
// Or git may not be using that directory at all, and that is R3: once
// core.hooksPath points elsewhere those files are already inert, so refusing
// over them would stop a repository with a missing shim from getting it back
// and leave no gate running at all. In that state the directory is not read —
// there is no question there to answer.
//
// THE DIRECTORY COMES BACK WHATEVER THE ANSWER, so a caller can name it in a
// message either way; only a failure to locate it at all leaves it empty.
//
// A LISTING FAILURE IS AN ERROR, NEVER AN EMPTY LIST. "No hooks here" and
// "formwork could not find out" are the two answers a refusal must not confuse,
// because only the first of them says it is safe to write. The single exception
// is a directory that does not exist, and it is an honest empty answer rather
// than a swallowed one: nothing can be running from a directory that is not
// there.
//
// THE EXCEPTION IS ASKED OF THE DIRECTORY, NOT OF THE ERROR realHooksIn RETURNS.
// Matching fs.ErrNotExist on that error caught a second, differently-shaped case
// as well: realHookFile stats each name the listing produced, so a file that
// vanishes between the two — or an access(2) answering ENOENT for it — surfaces
// the same sentinel. Read as "the directory does not exist", ONE missing file
// emptied the answer for every hook beside it, and the refusal built on this then
// had nothing to refuse over. Once the directory is known to be there, every
// errno propagates.
func preexistingHooks(root, hooksPath string) (string, []string, error) {
	dir, err := defaultHooksDir(root)
	if err != nil {
		return "", nil, err
	}
	// AN UNRESOLVABLE COMPARISON IS AN ERROR HERE, NOT AN "ELSEWHERE" (#171).
	// R3 below is a claim that these files are ALREADY inert, and the whole
	// refusal built on this detector rests on it; "formwork could not find out
	// which directory git runs hooks from" is not that claim. sameDirPath used to
	// fold the two together by answering false, and false is the answer that says
	// it is safe to write.
	same, err := sameDirPath(dir, hooksPath)
	if err != nil {
		return dir, nil, err
	}
	if !same {
		return dir, nil, nil // R3 — git runs hooks elsewhere, so these are inert
	}
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return dir, nil, nil
		}
		return dir, nil, err
	}
	names, err := realHooksIn(dir)
	if err != nil {
		return dir, nil, err
	}
	return dir, names, nil
}

// sameDirPath reports whether two directory paths name the same directory, and
// returns an error where it could not find out.
//
// Lexical first, symlinks second, and neither is os.SameFile: SameFile answers
// "same" for every pair on a filesystem without usable file IDs, which
// internal/vcs/vcs.go records at EnsureTopLevel's identity check as the reason
// that comparison is not the load-bearing one there.
//
// IT RETURNS AN ERROR RATHER THAN A GUESS, AND THAT IS #171. It moved here from
// verify.go when preexistingHooks became its second caller, and the two READ IT
// WITH OPPOSITE POLARITY: the shadowed-hooks report treats "not the same" as a
// reason to report, the detector above treats it as a reason not to refuse. So a
// single failure direction was safe for one caller and a fail-open for the
// other — install took over hook wiring it had never looked at, because a
// filesystem question could not be answered and "could not answer" was recorded
// as "no". A bool has nowhere to put that third state, so this returns it and
// each caller decides: install refuses (exit 2), verify reports (exit 1). The
// polarity split survives; what stops is one of the two paying for it silently.
//
// A PATH THAT IS NOT THERE IS STILL AN ANSWER, and it is `false`. One of the two
// directories does not exist, so they are not the same one — nothing was
// guessed, and turning that into an error would refuse every ordinary install,
// where formwork's own managed directory does not exist yet. Every OTHER
// resolution failure is a traversal this process could not perform, which is the
// state with no answer in it.
func sameDirPath(a, b string) (bool, error) {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true, nil
	}
	ra, exists, err := resolveDirPath(a)
	if err != nil || !exists {
		return false, err
	}
	rb, exists, err := resolveDirPath(b)
	if err != nil || !exists {
		return false, err
	}
	return ra == rb, nil
}

// resolveDirPath resolves one side of that comparison: the resolved path,
// whether it is there at all, and the failure that is neither.
func resolveDirPath(p string) (resolved string, exists bool, err error) {
	r, err := evalSymlinks(p)
	switch {
	case err == nil:
		return r, true, nil
	case errors.Is(err, fs.ErrNotExist):
		return "", false, nil
	default:
		return "", false, fmt.Errorf("cannot resolve %s: %w", p, err)
	}
}

// evalSymlinks is filepath.EvalSymlinks behind a variable, so a test can present
// the failure the two callers above disagree about.
//
// The seam is here for the reason `access` above is: the arm that must be pinned
// is the one that cannot be reached from outside. A path that is not there is
// constructible and is not the arm — it is an honest `false` — while a traversal
// the process cannot perform needs a directory permission a test cannot rely on
// (root ignores it, and the fixture would have to break the same .git the rest
// of the pre-flight reads). Without the seam the refusal that #171 adds ships
// unproven.
//
// resolvedKey in verify.go deliberately does NOT go through it: that key decides
// whether to REPEAT a check, never whether to perform one, so its failure
// direction is harmless by construction and forcing it would only make a dedupe
// test measure this variable instead.
var evalSymlinks = filepath.EvalSymlinks
