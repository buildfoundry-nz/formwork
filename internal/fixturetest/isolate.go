// isolate.go — a fixture for a process-bound rule is judged in its own
// repository (#28).
//
// The {{root}}/{{repo}} tokens stop a rule NAMING a tree outside itself. They
// do not stop git. Git discovers a repository by walking UP from the working
// directory, and a fixture tree has no .git, so a detector that runs
// `git rev-parse` inside one finds whatever repository encloses the corpus —
// and a `--default-range` arm then judges the real branch's commits, which is
// how a pass fixture came to fail for a commit that was never in it. No argv
// mentions that channel, so no argv rule can close it.
//
// It closes here instead: the arm is copied into a temp directory, that copy
// is `git init`-ed with one commit, and the copy is what the engine evaluates.
// Discovery stops at the fixture, and a `..` from the fixture root lands in an
// empty temp parent rather than in a repository.
//
// Only rules that SPAWN A PROCESS are isolated. A declarative rule reads the
// files the walk handed it and cannot ask git anything, so copying its
// fixtures would buy nothing and cost a tree copy per arm across a corpus
// where those are almost all of them.
package fixturetest

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
)

// gitPointerVars are the environment names that send git at a repository
// other than the one its working directory sits in. The materialising commands
// run without them: an ambient GIT_DIR (every git hook exports one) would
// otherwise commit the fixture's files into the caller's repository.
var gitPointerVars = []string{
	"GIT_DIR=", "GIT_WORK_TREE=", "GIT_COMMON_DIR=", "GIT_INDEX_FILE=",
	"GIT_OBJECT_DIRECTORY=", "GIT_ALTERNATE_OBJECT_DIRECTORIES=",
}

// isolationEnv is the ambient environment minus those pointers.
func isolationEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		keep := true
		for _, v := range gitPointerVars {
			if strings.HasPrefix(kv, v) {
				keep = false
				break
			}
		}
		if keep {
			env = append(env, kv)
		}
	}
	return env
}

// wantsIsolation reports whether this rule's fixtures are judged in an
// isolated repository. Two conditions, and both are necessary.
//
// It must EXEC. A declarative rule reads the files the walk handed it and
// cannot ask git anything, so a copy buys nothing and the corpus would pay a
// tree copy per arm for almost every rule it has. The test reads the rule
// TYPE, through the seam compile-once already uses — not
// Checker.ProcessBound, which answers whether the argv launches a heavyweight
// toolchain and is false for `sh -c`, the very way a detector asks git where
// it is.
//
// And it must NAME ITS TREE with a token. Isolation arrives WITH the
// migration rather than ahead of it: a rule whose argv still names paths
// relative to the caller's working directory is asking for the
// arm-inside-the-repository layout, and its detector routinely resolves a
// repo-resident helper by walking out of the arm. Severing that before the
// rule can say which tree to read turns a working fixture into a broken one
// for no gain — the rule cannot be told the fixture either way, so the
// isolation protects nothing it can act on. Measured downstream on the first
// whole-corpus run: a detector exiting 2 with "cannot locate the decomment
// wrapper at scripts/dev/decomment", a helper its arm never carried.
//
// A migrated rule is the one that can be handed a tree, so it is the one that
// gets a tree of its own. That makes the pairing mechanical: the argv the
// engine substitutes into is the argv the engine isolates.
func wantsIsolation(r *config.Rule) bool {
	cmd, ok := ruleCommandCmd(r)
	if !ok {
		return false
	}
	for _, a := range cmd {
		if strings.Contains(a, rootToken) || strings.Contains(a, repoToken) {
			return true
		}
	}
	return false
}

// The tokens, spelled here rather than imported, because internal/rules/command
// keeps them unexported and this package asks a different question of them: not
// what they resolve to, but whether a rule has adopted them.
const (
	rootToken = "{{root}}"
	repoToken = "{{repo}}"
)

// isolateArm materialises src as its own repository under a temp directory
// and returns the copy's path with a cleanup. The committed fixture tree is
// never written to — it is the author's evidence, not scratch space.
//
// A failure to materialise is returned, never swallowed: evaluating the
// original tree instead would be the escape this function exists to close,
// reappearing exactly when the isolation is broken.
func isolateArm(src string) (string, func(), error) {
	tmp, err := os.MkdirTemp("", "formwork-fixture-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(tmp) }
	// One level down, so `..` from the fixture root lands in an empty
	// directory rather than on the temp root's other tenants.
	dst := filepath.Join(tmp, "tree")
	if err := copyTree(src, dst); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("fixture isolation: copying %s: %w", src, err)
	}
	// The `.want` manifest is a SIBLING of the arm, deliberately outside the
	// scan root, so the tree copy above does not reach it. Left behind, the
	// arm arrives with no declared expectations and a fire fixture reads as
	// "declares no expectations" — the isolation silently disarming the proof
	// it was meant to protect.
	if err := copyFileIfPresent(src+".want", dst+".want"); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("fixture isolation: copying %s.want: %w", src, err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "-A"},
		{"-c", "user.name=formwork", "-c", "user.email=formwork@invalid",
			"-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "fixture"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dst}, args...)...)
		cmd.Env = isolationEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("fixture isolation: git %s in %s: %w\n%s", strings.Join(args, " "), src, err, out)
		}
	}
	return dst, cleanup, nil
}

// copyTree copies a directory tree, following no symlinks: a link inside a
// fixture is refused for the reason the discovery walk refuses one, and
// copying its target would silently pull in a file from outside the tree.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type()&os.ModeSymlink != 0:
			return fmt.Errorf("%s is a symlink — refused: copying its target would pull a file from outside the fixture into the tree under evaluation", p)
		case !d.Type().IsRegular():
			return fmt.Errorf("%s is not a regular file — refused: a fixture tree is files and directories", p)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(p, target, info.Mode().Perm())
	})
}

// copyFileIfPresent copies src to dst when src exists, and reports any other
// error. A missing manifest is the common case (markers instead), never a
// failure.
func copyFileIfPresent(src, dst string) error {
	info, err := os.Stat(src)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return copyFile(src, dst, info.Mode().Perm())
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// isolateArms materialises every arm of a process-bound rule and returns the
// arms with their evaluation paths set, plus one cleanup for all of them. For
// any other rule the arms keep their committed paths and the cleanup is a
// no-op.
func isolateArms(r *config.Rule, ruleDir string, arms []armEntry) ([]armEntry, func(), error) {
	out := make([]armEntry, len(arms))
	copy(out, arms)
	for i := range out {
		out[i].path = filepath.Join(ruleDir, out[i].name)
		out[i].src = out[i].path
	}
	if !wantsIsolation(r) {
		return out, func() {}, nil
	}
	var cleanups []func()
	cleanup := func() {
		for _, c := range cleanups {
			c()
		}
	}
	for i := range out {
		p, c, err := isolateArm(out[i].path)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		cleanups = append(cleanups, c)
		out[i].path = p
	}
	return out, cleanup, nil
}
