package fixturetest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/rules/command"
)

// goRunShape is a params.cmd that formwork test can compile once per rule.
type goRunShape struct {
	chDir     string   // -C directory relative to arm root; empty when absent
	buildArgs []string // packages/files passed to go build
	progArgs  []string // args after the package/files, forwarded to the binary
	treePaths []string // arm-relative paths that must be byte-identical across arms
}

// parseGoRunShape recognises the three corpus shapes:
//
//	go run file.go [file.go...] [args...]
//	go run -C dir pkg [args...]
//	go -C dir run pkg [args...]
func parseGoRunShape(cmd []string) (goRunShape, bool) {
	if len(cmd) < 3 || cmd[0] != "go" {
		return goRunShape{}, false
	}
	var chDir string
	var rest []string
	switch {
	case cmd[1] == "-C" && len(cmd) >= 5 && cmd[3] == "run":
		chDir = cmd[2]
		rest = cmd[4:]
	case cmd[1] == "run":
		rest = cmd[2:]
		if len(rest) >= 2 && rest[0] == "-C" {
			chDir = rest[1]
			rest = rest[2:]
		}
	default:
		return goRunShape{}, false
	}
	if len(rest) == 0 {
		return goRunShape{}, false
	}
	var buildArgs, progArgs []string
	if strings.HasSuffix(rest[0], ".go") {
		i := 0
		for i < len(rest) && strings.HasSuffix(rest[i], ".go") {
			buildArgs = append(buildArgs, rest[i])
			i++
		}
		progArgs = rest[i:]
	} else {
		buildArgs = []string{rest[0]}
		progArgs = rest[1:]
	}
	// Fingerprint what go build actually reads. With -C, buildArgs are relative
	// to chDir — resolve them to arm-relative paths. Do NOT substitute the whole
	// chDir tree: that both misses inputs outside chDir (../file.go) and falsely
	// mismatches when unrelated payload .go files differ under a wide -C root.
	treePaths := make([]string, 0, len(buildArgs))
	if chDir != "" {
		for _, a := range buildArgs {
			joined := filepath.Join(filepath.FromSlash(chDir), filepath.FromSlash(a))
			treePaths = append(treePaths, filepath.ToSlash(filepath.Clean(joined)))
		}
	} else {
		treePaths = append(treePaths, buildArgs...)
	}
	return goRunShape{chDir: chDir, buildArgs: buildArgs, progArgs: progArgs, treePaths: treePaths}, true
}

func ruleCommandCmd(r *config.Rule) ([]string, bool) {
	if r.Type != "command" {
		return nil, false
	}
	raw, err := r.Params()
	if err != nil || raw == nil {
		return nil, false
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	c, ok := m["cmd"].([]any)
	if !ok || len(c) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(c))
	for _, v := range c {
		s, ok := v.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// treeDigest returns a stable fingerprint of the detector paths under arm.
// A mismatch across arms is an engine error — never a silent fallthrough to
// a binary built from a different tree. Hashing errors are returned to the
// caller, which falls back to cold go run (same as build failure).
func treeDigest(arm string, shape goRunShape) (string, error) {
	return treeDigestRec(arm, shape, map[string]bool{})
}

func treeDigestRec(arm string, shape goRunShape, seenReplace map[string]bool) (string, error) {
	h := sha256.New()
	seenDir := map[string]bool{}
	for _, rel := range shape.treePaths {
		root := filepath.Join(arm, filepath.FromSlash(rel))
		info, err := os.Lstat(root)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("detector path %s is a symlink — refused", root)
		}
		if !info.IsDir() {
			if err := hashGoBuildFile(h, arm, rel, root); err != nil {
				return "", err
			}
			continue
		}
		seenDir[filepath.Clean(rel)] = true
		err = filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if fi.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("detector path %s is a symlink — refused", path)
			}
			if fi.IsDir() {
				// Mirror go tool ignore rules so payload under these dirs
				// cannot trip a compile-once mismatch the compiler never reads.
				base := fi.Name()
				if path != root && (base == "testdata" || strings.HasPrefix(base, "_") || strings.HasPrefix(base, ".")) {
					return filepath.SkipDir
				}
				return nil
			}
			// Only fingerprint what go build reads as source. Fixture payload
			// files that happen to sit beside the package (MARKER, sample SQL)
			// must not force a per-arm rebuild or trip the identity check.
			if !isGoBuildInput(fi.Name()) {
				return nil
			}
			relPath, err := filepath.Rel(arm, path)
			if err != nil {
				return err
			}
			return hashGoBuildFile(h, arm, filepath.ToSlash(relPath), path)
		})
		if err != nil {
			return "", err
		}
	}
	// Local module replaces are part of what go build reads. Prefer the -C
	// module directory (the go.mod go build actually loads); also fold replaces
	// declared beside any walked package directory.
	// File-list shapes (go run file.go) leave modDirs empty — arm-root go.mod
	// is not fingerprinted; only matters for non-stdlib detectors that read it.
	modDirs := map[string]bool{}
	if shape.chDir != "" {
		modDirs[filepath.Clean(shape.chDir)] = true
	}
	for d := range seenDir {
		modDirs[d] = true
	}
	modList := make([]string, 0, len(modDirs))
	for d := range modDirs {
		modList = append(modList, d)
	}
	sort.Strings(modList)
	for _, modRel := range modList {
		replaces, err := localReplaceDirs(filepath.Join(arm, filepath.FromSlash(modRel)))
		if err != nil {
			return "", err
		}
		for _, rep := range replaces {
			abs := filepath.Clean(rep)
			if seenReplace[abs] {
				return "", fmt.Errorf("replace cycle at %s", abs)
			}
			repRel, err := filepath.Rel(arm, abs)
			if err != nil {
				return "", err
			}
			// Keep the hashed tree self-contained: a replace that escapes the
			// arm (e.g. ../../shared) must not fingerprint files outside both
			// fixture roots — hash error → cold go-run fallback.
			if !filepath.IsLocal(repRel) {
				return "", fmt.Errorf("replace %s escapes arm root", abs)
			}
			seenReplace[abs] = true
			sub, err := treeDigestRec(arm, goRunShape{treePaths: []string{filepath.ToSlash(repRel)}}, seenReplace)
			delete(seenReplace, abs)
			if err != nil {
				return "", err
			}
			io.WriteString(h, "replace:"+filepath.ToSlash(repRel)+"="+sub+"\n")
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func isGoBuildInput(base string) bool {
	switch base {
	case "go.mod", "go.sum", "go.work", "go.work.sum":
		return true
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".go", ".s", ".c", ".h", ".cc", ".cpp", ".cxx", ".hh", ".hpp":
		return true
	}
	return false
}

func hashFile(h io.Writer, rel, abs string) error {
	io.WriteString(h, rel+"\n")
	f, err := os.Open(abs)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	io.WriteString(h, "\n")
	return nil
}

// hashGoBuildFile fingerprints a go-build input. For .go files it also folds
// //go:embed patterns in that file (relative to the source dir) so an embed-only
// change across arms cannot silently reuse the wrong binary. Glob/all: forms are
// hashed when filepath.Glob can expand them; exotic embed patterns that Glob
// cannot express are a documented residual — prefer identical embed trees.
func hashGoBuildFile(h io.Writer, arm, rel, abs string) error {
	if err := hashFile(h, rel, abs); err != nil {
		return err
	}
	if strings.ToLower(filepath.Ext(abs)) != ".go" {
		return nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	dir := filepath.Dir(abs)
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "//go:embed") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "//go:embed"))
		// Strip the optional all: prefix (Go 1.18+); patterns follow.
		fields := strings.Fields(rest)
		for _, pat := range fields {
			pat = strings.TrimPrefix(pat, "all:")
			if pat == "" {
				continue
			}
			matches, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(pat)))
			if err != nil {
				return fmt.Errorf("go:embed pattern %q in %s: %w", pat, rel, err)
			}
			sort.Strings(matches)
			for _, m := range matches {
				info, err := os.Lstat(m)
				if err != nil {
					return err
				}
				if info.Mode()&os.ModeSymlink != 0 {
					return fmt.Errorf("go:embed path %s is a symlink — refused", m)
				}
				if info.IsDir() {
					err = filepath.Walk(m, func(path string, fi os.FileInfo, err error) error {
						if err != nil {
							return err
						}
						if fi.Mode()&os.ModeSymlink != 0 || fi.IsDir() {
							if fi.Mode()&os.ModeSymlink != 0 {
								return fmt.Errorf("go:embed path %s is a symlink — refused", path)
							}
							return nil
						}
						relPath, err := filepath.Rel(arm, path)
						if err != nil {
							return err
						}
						io.WriteString(h, "embed:")
						return hashFile(h, filepath.ToSlash(relPath), path)
					})
					if err != nil {
						return err
					}
					continue
				}
				relPath, err := filepath.Rel(arm, m)
				if err != nil {
					return err
				}
				io.WriteString(h, "embed:")
				if err := hashFile(h, filepath.ToSlash(relPath), m); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// localReplaceDirs returns absolute directories named by replace => ./rel or
// replace => ../rel directives in dir/go.mod, when present. Module-path and
// versioned targets (replace m => github.com/x/y v1.2.3) are not filesystem
// paths and must not be walked.
func localReplaceDirs(dir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "replace ") {
			continue
		}
		// replace m => ../path  OR  replace m v => ../path
		parts := strings.Fields(line)
		arrow := -1
		for i, p := range parts {
			if p == "=>" {
				arrow = i
				break
			}
		}
		if arrow < 0 || arrow+1 >= len(parts) {
			continue
		}
		target := parts[arrow+1]
		if filepath.IsAbs(target) || strings.Contains(target, "://") {
			continue
		}
		// Go's local-replace grammar: "." or a relative path beginning with ./ or ../.
		if target != "." && !strings.HasPrefix(target, "./") && !strings.HasPrefix(target, "../") {
			continue
		}
		out = append(out, filepath.Clean(filepath.Join(dir, target)))
	}
	return out, nil
}

func buildDetectorOnce(arm string, shape goRunShape, outBin string) error {
	args := append([]string{"build", "-o", outBin}, shape.buildArgs...)
	cmd := exec.Command("go", args...)
	if shape.chDir != "" {
		cmd.Dir = filepath.Join(arm, filepath.FromSlash(shape.chDir))
	} else {
		cmd.Dir = arm
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go build %v (dir=%s): %w\n%s", args, cmd.Dir, err, out)
	}
	return nil
}

func runArmsCold(r *config.Rule, ruleDir string, arms []armEntry, workers int) (problems []string, count int, err error) {
	for _, a := range arms {
		count++
		ps, err := runFixture(r, filepath.Join(ruleDir, a.name), a.isFire, workers)
		if err != nil {
			return nil, count, err
		}
		for _, p := range ps {
			problems = append(problems, a.name+": "+p)
		}
	}
	return problems, count, nil
}

// runCommandFixturesCompileOnce builds a go-run detector once for the rule and
// reuses the binary across every arm. On hash or build failure it falls back to
// the cold go-run path so the arm still surfaces the toolchain/path error. Only
// a detector-tree digest mismatch across arms is an engine error (exit 2).
func runCommandFixturesCompileOnce(r *config.Rule, ruleDir string, arms []armEntry, shape goRunShape, workers int) (problems []string, count int, err error) {
	if len(arms) == 0 {
		return nil, 0, nil
	}
	first := filepath.Join(ruleDir, arms[0].name)
	wantDigest, err := treeDigest(first, shape)
	if err != nil {
		// Same posture as build failure: incomplete/broken detector trees are
		// per-arm findings via cold go run, not a suite-wide abort.
		return runArmsCold(r, ruleDir, arms, workers)
	}
	for _, a := range arms[1:] {
		armPath := filepath.Join(ruleDir, a.name)
		got, err := treeDigest(armPath, shape)
		if err != nil {
			return runArmsCold(r, ruleDir, arms, workers)
		}
		if got != wantDigest {
			return nil, 0, fmt.Errorf("fixtures: rule %s: detector tree for %v differs between %s and %s — compile-once requires byte-identical detector copies across arms (diff the paths and restore them, or split the rule)",
				r.ID, shape.treePaths, arms[0].name, a.name)
		}
	}

	cacheDir, err := os.MkdirTemp("", "formwork-compileonce-*")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(cacheDir)
	// Basename is not "detector" — tests must not key compile counts off it
	// (go build -o sets cmd.Dir for -C shapes, so -C never appears on argv).
	bin := filepath.Join(cacheDir, "oncebin")
	if err := buildDetectorOnce(first, shape, bin); err != nil {
		// Fall back to cold go run so a broken detector still fails the arm
		// with the toolchain's own message rather than a silent pass.
		return runArmsCold(r, ruleDir, arms, workers)
	}

	for _, a := range arms {
		count++
		armPath := filepath.Join(ruleDir, a.name)
		workDir := ""
		if shape.chDir != "" {
			workDir = filepath.Join(armPath, filepath.FromSlash(shape.chDir))
		}
		ps, err := runFixtureCompiled(r, armPath, a.isFire, workers, bin, shape.progArgs, workDir)
		if err != nil {
			return nil, count, err
		}
		for _, p := range ps {
			problems = append(problems, a.name+": "+p)
		}
	}
	return problems, count, nil
}

func runFixtureCompiled(r *config.Rule, dir string, isFire bool, workers int, binary string, progArgs []string, workDir string) ([]string, error) {
	fresh, err := r.Fresh()
	if err != nil {
		return nil, fmt.Errorf("fixture %s: %w", dir, err)
	}
	rewritten, ok := command.WithCompiledBinary(fresh.Checker, binary, progArgs, workDir)
	if !ok {
		return runFixture(r, dir, isFire, workers)
	}
	fresh = fresh.CloneWithChecker(rewritten)
	findings, fset, err := EvalIn(fresh, dir, workers)
	if err != nil {
		return nil, err
	}
	return judgeFixture(r, dir, isFire, findings, fset)
}
