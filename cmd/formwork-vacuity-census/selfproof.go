package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// selfProofVerdicts reports the rule whose only in-scope mutable subject is its
// own detector, and which carries no mutation spec to prove it anyway (#15103).
//
// THE SHAPE. tracked-sh-shrink-only was unprovable by construction and this
// census said "OK: every rule can fail". Its scope held two globs — `**/*.sh`,
// which matched nothing, and `scripts/dev/check-tracked-sh-census.go`, the
// program its own params.cmd runs. A mutation must bite inside the scope it
// defends, so the only specimen available was the detector's own source, and
// renaming that breaks params.cmd's argv: the rule exits 1 on "no such file",
// the right exit number for the wrong reason. That is the false green a proof
// exists to refuse, and the rule could not carry a proof that avoided it.
//
// WHY NOTHING SAW IT. Two exemptions met. The dead glob was waived from the
// per-glob EMPTY-GLOB arm by its `# glob-dead:` annotation, which is the
// documented opt-out and was correctly used. detectorWitnesses then returns nil
// because no in-scope file lives outside scripts/ or tools/ — "the rule's whole
// subject is the gate tree; a gate witness is correct" — which is the right
// call for the ~145 rules whose subject genuinely IS gate code. Each exemption
// is defensible alone; together they leave the rule unexamined. Only the
// mutation-proof runner objected, and it selects on the diff, so a rule sitting
// in this shape untouched is never asked the question at all.
//
// WHY THE SPEC IS THE DISCRIMINATOR, AND NOT THE SCOPE. "Every in-scope file is
// gate code" cannot separate the two populations: formwork-meta-tool-houses-
// no-new-check-shell, rule-has-mutation-proof and formwork-rules-not-vacuous
// all live entirely under scripts/ or tools/ and all carry PASSING proofs. What
// separates them is narrower and mechanical — whether any file the scope
// reaches is one the rule's own command does not run. When none is, the spec is
// the only remaining evidence that the rule can be falsified, so its absence is
// the finding. Ten of the thirteen rules in this shape in this corpus already
// carry one, by inverting the detector's own predicate and pinning the finding
// with expect_output, which is the cure this verdict names.
//
// Whether a spec that exists actually BITES is not asked here and must not be:
// that is the mutation-proof runner's verdict, reached by executing the proof
// against a scratch tree. The census asks the static half — does this rule have
// any evidence at all — which is the half that can be asked corpus-wide on
// every run rather than only on the rules a diff happens to touch.
func selfProofVerdicts(r *config.Rule, root string, inScope []*scan.File) []verdict {
	// type:command only. A declarative rule over its own origin is a different
	// animal: nothing EXECUTES the file — the engine reads it — so a content
	// mutation of that very file falsifies the rule cleanly, and rules like
	// dart-format-gate-batch-bounded (required-pattern over a gate's source)
	// are provable exactly that way.
	if r.Type != "command" || len(inScope) == 0 {
		return nil
	}
	machinery := ownMachinery(r, root)
	if len(machinery) == 0 {
		return nil // nothing resolvable to compare against; claim no verdict
	}
	residual := make([]string, 0, len(inScope))
	for _, f := range inScope {
		if !machinery[f.Path()] {
			residual = append(residual, f.Path())
		}
	}
	if len(residual) > 0 {
		// A library the detector LINKS is machinery one level down, and argv
		// never names it: count-relation-arm-is-anchored scopes its own module
		// plus tools/formworkcensus/**, which it imports. Resolving the link
		// costs a `go list`, so it is asked only when the residual is small
		// enough to be a gate library at all — bounded to the gate tree, which
		// leaves 18 rules in this corpus rather than 74.
		if !allInGateTree(residual) {
			return nil // a subject outside the gate fleet is a specimen a proof can use
		}
		linked, ok := linkedGatePackages(r, root)
		if !ok {
			return nil // the link could not be resolved; claim no verdict rather than guess
		}
		for _, p := range residual {
			if !linked[p] {
				return nil
			}
			machinery[p] = true
		}
	}
	trapped := make([]string, 0, len(inScope))
	for _, f := range inScope {
		trapped = append(trapped, f.Path())
	}
	if specPath := filepath.Join(root, ".formwork", "mutations", r.ID+".yaml"); fileExists(specPath) {
		return nil
	}
	sort.Strings(trapped)
	ev := make([]string, 0, len(trapped)+1)
	for _, p := range trapped {
		ev = append(ev, p+": in scope, and run by this rule's own params.cmd")
	}
	ev = append(ev, fmt.Sprintf(".formwork/mutations/%s.yaml: absent", r.ID))
	return []verdict{{
		class: class2, code: "SELF-DETECTOR-UNPROVEN", gating: true,
		why: "every file this rule's scope reaches is machinery its own params.cmd runs, so the only " +
			"mutation available breaks the invocation rather than the invariant — and no mutation spec " +
			"exists to show otherwise. Widen the scope to the subject the detector actually asserts " +
			"about, or add .formwork/mutations/" + r.ID + ".yaml inverting the detector's own predicate " +
			"and pinning the finding with expect_output",
		evidence: ev,
	}}
}

// allInGateTree reports whether every path lives under the gate directories.
//
// This is the bound on the link check, and it is the same gate-tree notion
// detectorWitnesses already uses. A detector importing a PRODUCT package does
// not thereby make that package machinery — the package is the rule's subject
// and mutating it is the proof, which is what break-glass-consults-operators-row
// and the other product rules that link their own detector rely on. Only the
// gate fleet's shared code is machinery one level down.
func allInGateTree(paths []string) bool {
	for _, p := range paths {
		if !strings.HasPrefix(p, "scripts/") && !strings.HasPrefix(p, "tools/") {
			return false
		}
	}
	return true
}

// linkedGatePackages returns the files of every in-repo gate-tree package the
// rule's command compiles, resolved with `go list -deps` — the compiler's own
// answer rather than an import-graph re-implementation.
//
// The bool is false when the link cannot be resolved (no module target, or the
// toolchain refuses). The caller then claims NO verdict: an instrument that
// could not evaluate must not report a rule as unprovable on a guess, and the
// mutation-proof runner still covers the rules a diff touches.
func linkedGatePackages(r *config.Rule, root string) (map[string]bool, bool) {
	cmd := commandArgv(r)
	if len(cmd) == 0 || cmd[0] != "go" {
		return nil, false
	}
	base := ""
	for i := 0; i < len(cmd); i++ {
		if cmd[i] == "-C" && i+1 < len(cmd) {
			base = strings.TrimSuffix(path.Clean(cmd[i+1]), "/")
			if base == "." {
				base = ""
			}
			i++
		}
	}
	targets := runTargets(cmd)
	if len(targets) == 0 {
		return nil, false
	}
	args := append([]string{"list", "-deps", "-f", "{{if .Module}}{{.Dir}}{{end}}"}, targets...)
	c := exec.Command("go", args...)
	c.Dir = filepath.Join(root, filepath.FromSlash(base))
	out, err := c.Output()
	if err != nil {
		return nil, false
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, false
	}
	files := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		dir := strings.TrimSpace(line)
		if dir == "" {
			continue
		}
		rel, err := filepath.Rel(abs, dir)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue // outside the repo: the module cache, the standard library
		}
		relSlash := filepath.ToSlash(rel)
		if !strings.HasPrefix(relSlash, "scripts/") && !strings.HasPrefix(relSlash, "tools/") {
			continue
		}
		for _, p := range packageFiles(root, relSlash) {
			files[p] = true
		}
	}
	return files, true
}

// ownMachinery is the set of repo-relative paths the rule's params.cmd compiles
// or executes, plus its declared origin.
//
// Resolution follows the shapes formwork-origin-fidelity already sanctions for
// a command rule: [<origin>], [go, run, <file.go>…, …], [go, run, -C, <dir>, .,
// …] and [go, -C, <dir>, run, ., …], plus the dart equivalent. `-C` is read
// wherever it appears because both spellings are live in this corpus.
//
// The `run .` form names a DIRECTORY, and the machinery it pulls in is that
// package's own files plus the module files beside them — not the whole tree
// beneath the -C directory. `go -C api-factory run ./internal/cdpdrive/
// checkwalkmapbounds` compiles one package; reading it as all of api-factory
// would sweep every product file the rule guards into "machinery" and silence
// the arm on precisely the rules it is for.
func ownMachinery(r *config.Rule, root string) map[string]bool {
	out := map[string]bool{}
	if r.Origin != "" && fileExists(filepath.Join(root, filepath.FromSlash(r.Origin))) {
		out[r.Origin] = true
	}
	cmd := commandArgv(r)
	if len(cmd) == 0 {
		return out
	}
	base := ""
	for i := 0; i < len(cmd); i++ {
		if cmd[i] == "-C" && i+1 < len(cmd) {
			base = strings.TrimSuffix(path.Clean(cmd[i+1]), "/")
			if base == "." {
				base = ""
			}
			i++
		}
	}
	// A non-`go`/`dart` argv is a program path invoked directly: every argument
	// that resolves to a real file is machinery (the detector, and the helper
	// sources a single-file go-run detector co-compiles).
	if cmd[0] != "go" && cmd[0] != "dart" {
		for _, a := range cmd {
			if p := path.Clean(a); fileExists(filepath.Join(root, filepath.FromSlash(p))) {
				out[p] = true
			}
		}
		return out
	}
	targets := runTargets(cmd)
	for _, t := range targets {
		switch {
		case strings.HasSuffix(t, ".go") || strings.HasSuffix(t, ".dart"):
			p := path.Clean(path.Join(base, t))
			if fileExists(filepath.Join(root, filepath.FromSlash(p))) {
				out[p] = true
			}
		default: // a package directory: "." or "./sub/pkg"
			dir := path.Clean(path.Join(base, t))
			if dir == "." {
				dir = ""
			}
			for _, p := range packageFiles(root, dir) {
				out[p] = true
			}
			// go.mod/go.sum sit at the MODULE root, which is the -C directory
			// when the target is a subpackage of it.
			for _, d := range []string{dir, base} {
				for _, name := range []string{"go.mod", "go.sum", "pubspec.yaml"} {
					p := path.Clean(path.Join(d, name))
					if fileExists(filepath.Join(root, filepath.FromSlash(p))) {
						out[p] = true
					}
				}
			}
		}
	}
	return out
}

// runTargets returns the package or source files the toolchain compiles: the
// arguments after the `run` verb, with go's own flags skipped.
//
// Two shapes in this corpus break a naive scan. `-C` is written on both sides
// of the verb — 42 rules before it, 8 after — and a reader that stops at the
// first dash never reaches the package on those 8, so the detector's own module
// reads as an outside witness and the arm goes silent on exactly the rules it
// is for. And every go-run detector here is handed the repo root as its first
// argument, spelled `../..`; resolving that as a package path walks out of the
// tree and swallows whatever it lands on. A package target is `.` or `./sub`
// and nothing else.
func runTargets(cmd []string) []string {
	i := 0
	for ; i < len(cmd); i++ {
		if cmd[i] == "run" {
			i++
			break
		}
	}
	var out []string
	for ; i < len(cmd); i++ {
		a := cmd[i]
		if strings.HasPrefix(a, "-") {
			// -C takes a separate value; -flag=value carries its own.
			if a == "-C" || (!strings.Contains(a, "=") && goRunFlagsWithValue[a]) {
				i++
			}
			continue
		}
		if strings.HasSuffix(a, ".go") || strings.HasSuffix(a, ".dart") {
			out = append(out, a)
			continue
		}
		if a == "." || strings.HasPrefix(a, "./") {
			out = append(out, a)
		}
		// The first argument that is neither a source file nor a module-relative
		// package path is the detector's own first argument (this repo's
		// detectors take the repo root), so the compiled set is complete.
		break
	}
	return out
}

// goRunFlagsWithValue are the `go run` flags whose value is a separate argv
// entry, so the resolver knows to step over it rather than read it as a target.
var goRunFlagsWithValue = map[string]bool{
	"-C": true, "-exec": true, "-tags": true, "-ldflags": true,
	"-gcflags": true, "-asmflags": true, "-mod": true, "-overlay": true,
	"-p": true, "-covermode": true, "-coverpkg": true, "-pgo": true,
}

// packageFiles lists the regular files directly inside dir, repo-relative. A Go
// package is one directory deep by definition, so the walk deliberately does
// not recurse.
func packageFiles(root, dir string) []string {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out = append(out, path.Join(dir, e.Name()))
	}
	return out
}

// commandArgv reads params.cmd off the rule as written. Only metadata is read;
// nothing here executes the detector.
func commandArgv(r *config.Rule) []string {
	raw, err := r.ParamsYAML()
	if err != nil || raw == "" {
		return nil
	}
	var p struct {
		Cmd []string `yaml:"cmd"`
	}
	if err := yaml.Unmarshal([]byte(raw), &p); err != nil {
		return nil
	}
	return p.Cmd
}

// fileExists reports whether abs names a regular file.
func fileExists(abs string) bool {
	st, err := os.Stat(abs)
	return err == nil && st.Mode().IsRegular()
}
