package vcs

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// IgnoredPath is one path git ignores, with the ignore rule responsible for
// it. Path is repo-relative and slash-separated with NO trailing slash; Dir
// says whether it names a directory, which the walk may prune without
// descending.
type IgnoredPath struct {
	Path    string
	Dir     bool
	Source  string // the ignore file that decided it, e.g. ".gitignore"
	Line    int    // 1-based line number within Source
	Pattern string // the pattern on that line
}

// Rule renders the deciding ignore line in the shape git's own
// `check-ignore -v` prints it ("<file>:<line>:<pattern>"), so a reader can
// paste it straight back at git. Every consumer that attributes a prune —
// the census, rules-for — uses this one spelling.
func (p IgnoredPath) Rule() string {
	return fmt.Sprintf("%s:%d:%s", p.Source, p.Line, p.Pattern)
}

// IgnoredUnder returns every path git ignores at or below root, sorted by
// Path, each carrying the ignore rule responsible for it. Like TrackedUnder it
// does not require root to be the repository top-level: git reports paths
// relative to its cwd, which is the frame the scan's root-relative paths use.
//
// WHY THIS IS THE IGNORED SET AND NOT THE UNTRACKED SET. An untracked file is
// still compiled by the toolchain and is usually someone's work in progress,
// so skipping it would be a rule bypass on the file most likely to matter
// (#80, #54). An IGNORED file is different in kind: git will not accept it
// without an edit to .gitignore or an explicit `add -f`, so no rule can be
// evading anything by declining to read it. This function draws exactly that
// line and no wider one.
//
// TWO CALLS, AND THE SECOND IS THE SAFETY PROPERTY.
//
//  1. `ls-files --others --ignored --exclude-standard --directory` proposes
//     candidates. --directory collapses a wholly-untracked directory to a
//     single entry so a caller can prune the subtree without enumerating it —
//     the whole reason this is affordable on a tree carrying many ignored
//     checkouts.
//  2. `check-ignore` confirms each candidate, and only confirmed paths are
//     returned.
//
// The confirmation is not belt-and-braces; step 1 over-reports in two ways
// that would each be a silent coverage hole:
//
//   - --directory collapses a directory that is merely untracked-throughout,
//     not ignored. A tree holding only a/b/ignored/ reports a/ and a/b/ as
//     well, and pruning those would skip paths no .gitignore line matches.
//   - check-ignore WITHOUT --no-index refuses to call a tracked path ignored.
//     That is git's own rule, and leaning on it means a force-added file under
//     an ignored directory cannot be pruned by any bug in this package. Never
//     add --no-index here: it turns the guarantee off.
//
// Both git calls are -z. With default core.quotePath git C-quotes any path
// holding a non-ASCII or control byte, and a quoted spelling can never match a
// scan path — the same silent-miss #96 found in the --staged/--range listing.
//
// BOTH ALSO READ AN INDEX, AND GIT_INDEX_FILE MOVES IT (#175). ls-files decides
// the collapse from the index and check-ignore decides the tracked carve-out
// from it, so a moved index breaks the guarantee at BOTH steps — which is why
// the cross-check runs the whole pair again rather than only the confirmation.
// The prune set is then the paths both indexes call ignored (keepAgreedPrunes,
// indexguard.go), and it is skipped entirely — no extra git calls — when
// GIT_INDEX_FILE is unset.
//
// A repo where nothing is ignored is not an error: check-ignore exits 1 to say
// "none of these", which is an answer. Every other failure is returned, and
// callers must not read an error as "nothing is ignored" (package contract) —
// including a failure of the cross-check's own run.
func IgnoredUnder(root string) ([]IgnoredPath, error) {
	ambient, err := ignoredUnder(root, ambientIndex)
	if err != nil {
		return nil, err
	}
	if !indexMoved() {
		return ambient, nil
	}
	repoSide, err := ignoredUnder(root, repositoryIndex)
	if err != nil {
		return nil, err
	}
	return keepAgreedPrunes(repoSide, ambient), nil
}

// ignoredUnder is IgnoredUnder's two-step against ONE index frame, which is the
// unit the #175 cross-check runs twice.
func ignoredUnder(root string, frame indexFrame) ([]IgnoredPath, error) {
	out, err := frame.git(root, "ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--directory")
	if err != nil {
		return nil, err
	}
	var candidates []string
	for _, rec := range strings.Split(out, "\x00") {
		if rec != "" {
			candidates = append(candidates, filepath.ToSlash(rec))
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	return confirmIgnored(root, candidates, frame)
}

// CheckIgnored asks git whether it would ignore each of the given paths —
// which need not exist. IgnoredUnder snapshots the paths that exist in the
// working tree, so it can never answer for a file about to be written;
// check-ignore evaluates the ignore patterns for any pathname, closing that
// gap (#122's ghost frame), and one --stdin call serves the whole batch. A
// path absent from the result is the "not ignored" verdict. The tracked
// carve-out IgnoredUnder leans on holds here for the same reason: no
// --no-index, so git refuses to call a tracked path ignored — subject to the
// same #175 qualification IgnoredUnder carries, and closed the same way: where
// GIT_INDEX_FILE moves the index, the answer is the paths BOTH indexes call
// ignored. There is no ls-files step to repeat here, because the candidates come
// from the caller rather than from a listing. An unanswerable question (no
// repository, no git) is an error, never "not ignored" (package contract).
func CheckIgnored(root string, paths []string) ([]IgnoredPath, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	ambient, err := confirmIgnored(root, paths, ambientIndex)
	if err != nil {
		return nil, err
	}
	if !indexMoved() {
		return ambient, nil
	}
	repoSide, err := confirmIgnored(root, paths, repositoryIndex)
	if err != nil {
		return nil, err
	}
	return keepAgreedPrunes(repoSide, ambient), nil
}

// confirmIgnored keeps only the candidates git actually ignores, pairing each
// with the rule that decided it. `check-ignore -z -v` emits four
// NUL-terminated fields per confirmed path — source, line, pattern, pathname —
// and omits the rest, so the filtering and the provenance come from one call.
func confirmIgnored(root string, candidates []string, frame indexFrame) ([]IgnoredPath, error) {
	stdin := strings.Join(candidates, "\x00") + "\x00"
	out, code, err := frame.gitStdin(root, stdin, "check-ignore", "-z", "-v", "--stdin")
	if err != nil {
		// Exit 1 is check-ignore's "none of the given paths are ignored". It is
		// a verdict, not a failure; every other code is a real error and is
		// surfaced, never softened into an empty result.
		if code == 1 {
			return nil, nil
		}
		return nil, err
	}
	fields := strings.Split(out, "\x00")
	var recs []IgnoredPath
	for i := 0; i+3 < len(fields); i += 4 {
		source, lineStr, pattern, path := fields[i], fields[i+1], fields[i+2], fields[i+3]
		if path == "" {
			continue
		}
		line, convErr := strconv.Atoi(lineStr)
		if convErr != nil {
			return nil, fmt.Errorf("git check-ignore: malformed line number %q for %s", lineStr, path)
		}
		// A record whose deciding pattern is a NEGATION is git's way of
		// saying the path is explicitly NOT ignored — check-ignore -v emits
		// it and exits 0 all the same. Keeping it would turn "git will track
		// this" into an ignore verdict: for IgnoredUnder the ls-files
		// pre-vetting makes such records impossible (a path whose deciding
		// rule is negated is not in the --ignored listing), but CheckIgnored
		// feeds unvetted candidates, where the misread told a guidance
		// consumer "not scanned" about a path the walk scans and enforces on
		// (#125 review finding 1).
		if strings.HasPrefix(pattern, "!") {
			continue
		}
		p := filepath.ToSlash(path)
		dir := strings.HasSuffix(p, "/")
		recs = append(recs, IgnoredPath{
			Path:    strings.TrimSuffix(p, "/"),
			Dir:     dir,
			Source:  filepath.ToSlash(source),
			Line:    line,
			Pattern: pattern,
		})
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Path < recs[j].Path })
	return recs, nil
}

// gitStdin runs `git -C root <args>` with input on stdin, returning stdout,
// the process exit code, and an error on any non-zero exit. The exit code is
// returned alongside the error because check-ignore's 1 means "no matches",
// which callers must distinguish from a real failure — the plain git helper
// collapses every non-zero into one error and cannot express that.
//
// It is the SECOND of this package's two exec sites and carries the same
// environment policy as gitExit (env.go), independently: this function shares
// no code path with that one, so the policy has to be applied here too or
// check-ignore alone would keep answering from an ambient GIT_DIR.
func gitStdin(root, input string, args ...string) (string, int, error) {
	env, envErr := gitEnvFor(root)
	if envErr != nil {
		return "", -1, envErr
	}
	return gitStdinEnv(env, root, input, args...)
}

// gitStdinEnv is gitStdin with the environment already decided — the stdin
// analogue of gitExitEnv (vcs.go), and it exists for the same reason: the #175
// cross-check runs the SAME question under two environments, so the environment
// has to be a parameter rather than something resolved inside. A nil env is
// os/exec's own meaning, "inherit the parent's unchanged", which is what
// gitEnvFor returns under the FORMWORK_GIT_ENV hatch.
func gitStdinEnv(env []string, root, input string, args ...string) (string, int, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Stdin = strings.NewReader(input)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", ee.ExitCode(), fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", -1, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), 0, nil
}

// Submodules lists the repository's registered submodule paths (gitlink
// entries, mode 160000), sorted, repo-relative and slash-separated. The
// guidance layer prefilters its check-ignore candidates through this list:
// check-ignore FATALS (exit 128, "Pathspec ... is in submodule") on any
// pathspec under a registered submodule — even one whose working tree was
// removed — while formwork's walk is submodule-OBLIVIOUS: it scans whatever
// plain files sit there and IgnoredUnder never prunes them, so for this
// engine a path under a submodule is simply not git-ignored. An unanswerable
// question (no repository, no git) is an error, never "no submodules".
func Submodules(root string) ([]string, error) {
	out, err := git(root, "ls-files", "-z", "--stage")
	if err != nil {
		return nil, err
	}
	var subs []string
	for _, rec := range strings.Split(out, "\x00") {
		// Each record: "<mode> <object> <stage>\t<path>".
		if !strings.HasPrefix(rec, "160000 ") {
			continue
		}
		if i := strings.IndexByte(rec, '\t'); i >= 0 {
			subs = append(subs, filepath.ToSlash(rec[i+1:]))
		}
	}
	sort.Strings(subs)
	return subs, nil
}
