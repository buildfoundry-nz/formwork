package gotypes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/buildfoundry-nz/formwork/internal/cputime"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// vettool is the executable `go vet` runs as its vettool: the formwork binary
// itself. A variable so the package's tests can run under their own binary,
// which TestMain turns into the vettool the same way main does.
var vettool = os.Executable

// rule is one go/types rule. CheckFile only records the in-scope Go files;
// FinalizeErr runs `go vet` over their packages, once per module, and keeps
// the diagnostics positioned in a recorded file.
type rule struct {
	// Meter carries go vet's CPU, which runs off the calling thread, to the
	// rule that started it.
	*cputime.Meter
	plan []byte

	mu    sync.Mutex
	files map[string]bool // slash paths relative to the root
}

// newForbiddenTypeRule is the factory for go/forbidden-type-in-position.
func newForbiddenTypeRule(node *yaml.Node) (rules.Checker, error) {
	var p forbiddenTypeParams
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, fmt.Errorf("go/forbidden-type-in-position: %w", err)
	}
	return newRule("forbidden-type-in-position", p)
}

// newRule builds the rule for one analyzer. The params are decoded by the
// analyzer here, at load, exactly as the vettool will decode them, so a rule
// the vettool would refuse is a config error before any `go vet` runs.
func newRule(analyzer string, params any) (rules.Checker, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("go/%s: params: %w", analyzer, err)
	}
	if _, err := analyzers[analyzer](raw); err != nil {
		return nil, fmt.Errorf("go/%s: %w", analyzer, err)
	}
	p, err := json.Marshal(plan{Analyzer: analyzer, Params: raw})
	if err != nil {
		return nil, err
	}
	return &rule{Meter: new(cputime.Meter), plan: p, files: map[string]bool{}}, nil
}

func init() {
	rules.Register("go/forbidden-type-in-position", newForbiddenTypeRule)
}

func (r *rule) CheckFile(f *scan.File) ([]rules.Match, error) {
	if strings.HasSuffix(f.Path(), ".go") {
		r.mu.Lock()
		r.files[filepath.ToSlash(f.Path())] = true
		r.mu.Unlock()
	}
	return nil, nil
}

// Cost is heavy: the rule runs the go command, as a command rule does.
func (r *rule) Cost() rules.Cost { return rules.CostHeavy }

// ProcessBound puts the rule in the width-capped analyzer pool: `go vet`
// type-checks every package it is given.
func (r *rule) ProcessBound() bool { return true }

func (r *rule) FinalizeErr(ctx rules.FinalizeContext) ([]rules.Match, error) {
	r.mu.Lock()
	given := make(map[string]bool, len(r.files))
	for f := range r.files {
		given[f] = true
	}
	r.mu.Unlock()
	if len(given) == 0 {
		return nil, nil
	}
	files := sortedKeys(given)
	// go vet prints absolute positions; a relative root (`check` without -C
	// evaluates ".") is made absolute so they can be read back against it.
	root, err := filepath.Abs(ctx.Root)
	if err != nil {
		return nil, err
	}
	modules, err := packagesByModule(root, files)
	if err != nil {
		return nil, err
	}
	exe, err := vettool()
	if err != nil {
		return nil, fmt.Errorf("locate the formwork binary for go vet: %w", err)
	}
	var matches []rules.Match
	for _, mod := range sortedKeys(modules) {
		found, err := r.vet(exe, root, mod, modules[mod])
		if err != nil {
			return nil, err
		}
		matches = append(matches, found...)
	}
	keep := matches[:0]
	for _, m := range matches {
		if given[m.Path] {
			keep = append(keep, m)
		}
	}
	sort.Slice(keep, func(i, j int) bool {
		if keep[i].Path != keep[j].Path {
			return keep[i].Path < keep[j].Path
		}
		return keep[i].Line < keep[j].Line
	})
	return keep, nil
}

// packagesByModule maps each module directory (slash, relative to root; "."
// for root) to the package directories, relative to it, that hold the files.
// A file with no go.mod above it inside root cannot be type-checked, and is an
// error rather than a file nobody looked at.
func packagesByModule(root string, files []string) (map[string][]string, error) {
	modOf := map[string]string{}
	modules := map[string]map[string]bool{}
	for _, f := range files {
		dir := path.Dir(f)
		mod, ok := modOf[dir]
		if !ok {
			mod, ok = moduleFor(root, dir)
			if !ok {
				return nil, fmt.Errorf("%s: no go.mod above it inside the root, so go vet cannot type-check it", f)
			}
			modOf[dir] = mod
		}
		rel := "."
		switch {
		case dir == mod:
		case mod == ".":
			rel = "./" + dir
		default:
			rel = "./" + strings.TrimPrefix(dir, mod+"/")
		}
		if modules[mod] == nil {
			modules[mod] = map[string]bool{}
		}
		modules[mod][rel] = true
	}
	out := map[string][]string{}
	for mod, pkgs := range modules {
		out[mod] = sortedKeys(pkgs)
	}
	return out, nil
}

func moduleFor(root, dir string) (string, bool) {
	for {
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(dir), "go.mod")); err == nil && info.Mode().IsRegular() {
			return dir, true
		}
		if dir == "." {
			return "", false
		}
		dir = path.Dir(dir)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// vet runs one `go vet` for one module. A non-zero exit is a package that did
// not build or type-check, and an "error" in the report is an analyzer that
// failed: either is an engine error, never a clean result.
func (r *rule) vet(exe, root, mod string, pkgs []string) ([]rules.Match, error) {
	dir := filepath.Join(root, filepath.FromSlash(mod))
	args := append([]string{"vet", "-vettool=" + exe, "-formwork.plan=" + string(r.plan), "-json"}, pkgs...)
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), VettoolEnv+"=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cputime.Run(r.Meter, cmd); err != nil {
		return nil, fmt.Errorf("go vet in %s: %v\n%s", mod, err, strings.TrimSpace(stderr.String()))
	}
	return parseVetJSON(root, &stdout)
}

type vetDiagnostic struct {
	Posn    string `json:"posn"`
	Message string `json:"message"`
}

// parseVetJSON reads the stream of per-package objects `go vet -json` prints:
// package path, then analyzer name, then a list of diagnostics or an error.
func parseVetJSON(root string, r io.Reader) ([]rules.Match, error) {
	dec := json.NewDecoder(r)
	var matches []rules.Match
	for {
		var tree map[string]map[string]json.RawMessage
		if err := dec.Decode(&tree); err == io.EOF {
			return matches, nil
		} else if err != nil {
			return nil, fmt.Errorf("go vet -json: %w", err)
		}
		for _, pkg := range sortedKeys(tree) {
			raw, ok := tree[pkg][Analyzer.Name]
			if !ok {
				continue
			}
			var failed struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(raw, &failed) == nil && failed.Error != "" {
				return nil, fmt.Errorf("go vet: %s: %s", pkg, failed.Error)
			}
			var diags []vetDiagnostic
			if err := json.Unmarshal(raw, &diags); err != nil {
				return nil, fmt.Errorf("go vet -json: %s: %w", pkg, err)
			}
			for _, d := range diags {
				m, err := matchAt(root, d)
				if err != nil {
					return nil, err
				}
				matches = append(matches, m)
			}
		}
	}
}

// matchAt turns a "file:line:col" position into a match on the root-relative
// path. A position outside the root is an error: no file there was given.
func matchAt(root string, d vetDiagnostic) (rules.Match, error) {
	file, line, ok := splitPosn(d.Posn)
	if !ok {
		return rules.Match{}, fmt.Errorf("go vet -json: unreadable position %q", d.Posn)
	}
	rel, err := filepath.Rel(root, file)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rules.Match{}, errors.New("go vet -json: position " + d.Posn + " is outside the root")
	}
	return rules.Match{Path: filepath.ToSlash(rel), Line: line, Message: d.Message}, nil
}

func splitPosn(posn string) (string, int, bool) {
	colon := strings.LastIndexByte(posn, ':')
	if colon < 0 {
		return "", 0, false
	}
	rest := posn[:colon]
	colon = strings.LastIndexByte(rest, ':')
	if colon < 0 {
		return "", 0, false
	}
	line, err := strconv.Atoi(rest[colon+1:])
	if err != nil {
		return "", 0, false
	}
	return rest[:colon], line, true
}
