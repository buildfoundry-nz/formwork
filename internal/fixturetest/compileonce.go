package fixturetest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	treePaths := append([]string(nil), buildArgs...)
	if chDir != "" {
		treePaths = []string{chDir}
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
// a binary built from a different tree.
func treeDigest(arm string, paths []string) (string, error) {
	h := sha256.New()
	for _, rel := range paths {
		root := filepath.Join(arm, filepath.FromSlash(rel))
		info, err := os.Lstat(root)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("detector path %s is a symlink — refused", root)
		}
		if !info.IsDir() {
			if err := hashFile(h, rel, root); err != nil {
				return "", err
			}
			continue
		}
		err = filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if fi.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("detector path %s is a symlink — refused", path)
			}
			if fi.IsDir() {
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
			return hashFile(h, filepath.ToSlash(relPath), path)
		})
		if err != nil {
			return "", err
		}
		// Local module replaces are part of what go build reads.
		if replaces, err := localReplaceDirs(root); err != nil {
			return "", err
		} else {
			for _, rep := range replaces {
				repRel, err := filepath.Rel(arm, rep)
				if err != nil {
					return "", err
				}
				sub, err := treeDigest(arm, []string{filepath.ToSlash(repRel)})
				if err != nil {
					return "", err
				}
				io.WriteString(h, "replace:"+filepath.ToSlash(repRel)+"="+sub+"\n")
			}
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

// localReplaceDirs returns absolute directories named by replace => ../rel
// directives in dir/go.mod, when present.
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

// runCommandFixturesCompileOnce builds a go-run detector once for the rule and
// reuses the binary across every arm. On build failure it falls back to the
// cold go-run path so the arm still surfaces the compile error. A detector-tree
// mismatch across arms is an engine error (exit 2), never a silent fallthrough.
func runCommandFixturesCompileOnce(r *config.Rule, ruleDir string, arms []armEntry, shape goRunShape, workers int) (problems []string, count int, err error) {
	if len(arms) == 0 {
		return nil, 0, nil
	}
	first := filepath.Join(ruleDir, arms[0].name)
	wantDigest, err := treeDigest(first, shape.treePaths)
	if err != nil {
		return nil, 0, fmt.Errorf("fixtures: %s: hashing detector tree: %w", first, err)
	}
	for _, a := range arms[1:] {
		armPath := filepath.Join(ruleDir, a.name)
		got, err := treeDigest(armPath, shape.treePaths)
		if err != nil {
			return nil, 0, fmt.Errorf("fixtures: %s: hashing detector tree: %w", armPath, err)
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
	bin := filepath.Join(cacheDir, "detector")
	if err := buildDetectorOnce(first, shape, bin); err != nil {
		// Fall back to cold go run so a broken detector still fails the arm
		// with the toolchain's own message rather than a silent pass.
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
