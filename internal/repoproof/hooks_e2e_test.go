// hooks_e2e_test.go — the built binary's git-hook wiring actually gates a real
// commit. Converted from scripts/hooks-e2e-proof.sh.
//
// WHY THIS EXISTS. No target ran the `formwork hooks` COMMAND until this one.
// What ships is a subcommand an operator types, which parses flags, loads rule
// YAML through the type registry, and returns an exit code — none of which the
// unit tests exercise together, and none of which proves a commit is actually
// refused. This drives real `git commit` calls against a real repository.
package repoproof_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// repo is a throwaway git repository the proof drives, plus the throwaway HOME
// it drives it under.
//
// home is a SIBLING of the work tree rather than a directory inside it: `git
// add -A` takes everything under the work tree, so a HOME nested there would
// commit itself and make the tracked corpus something other than the corpus
// this proof claims to be running.
type repo struct {
	t    *testing.T
	root string // holds the work tree and the throwaway HOME side by side
	dir  string // the work tree
	home string // the throwaway HOME; XDG_CONFIG_HOME is <home>/.config
	fw   string
}

// env is the environment every child process gets, in one place so the shim's
// git, the binary run directly, and a command built inline cannot drift into
// three different isolations.
//
// A throwaway HOME, with XDG_CONFIG_HOME under it, because GIT_CONFIG_GLOBAL
// covers ~/.gitconfig and nothing else. Git reads its global EXCLUDES file from
// $XDG_CONFIG_HOME/git/ignore, falling back to $HOME/.config/git/ignore, and
// that file decides what `git add -A` puts into this proof's corpus — a
// developer who globally ignores *.txt would have emptied the pre-port corpus
// of its fixtures and left the proof measuring a repository it half-built.
// os/exec keeps the last spelling of a duplicated variable, so these override
// the inherited ones rather than sitting behind them.
func (r *repo) env(extra ...string) []string {
	e := append(os.Environ(),
		"HOME="+r.home,
		"XDG_CONFIG_HOME="+filepath.Join(r.home, ".config"),
		"GIT_CONFIG_GLOBAL="+filepath.Join(r.home, ".gitconfig"),
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	return append(e, extra...)
}

func (r *repo) git(args ...string) (string, int) {
	r.t.Helper()
	c := exec.Command("git", args...)
	c.Dir = r.dir
	// The installed shim invokes `formwork` from PATH — that is the point of a
	// committed, machine-independent shim — so the binary under test has to BE
	// the one on PATH. Without this the commit is still refused, but by
	// "exec: formwork: not found", which is why every assertion below names the
	// reason rather than settling for a non-zero status.
	c.Env = r.env(
		"PATH="+filepath.Dir(r.fw)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_AUTHOR_NAME=hooks proof", "GIT_AUTHOR_EMAIL=h@example.invalid",
		"GIT_COMMITTER_NAME=hooks proof", "GIT_COMMITTER_EMAIL=h@example.invalid",
	)
	out, err := c.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return string(out), code
}

func (r *repo) fwRun(args ...string) (string, int) {
	r.t.Helper()
	c := exec.Command(r.fw, args...)
	c.Dir = r.dir
	c.Env = r.env()
	out, err := c.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return string(out), code
}

func (r *repo) write(rel, body string) {
	r.t.Helper()
	p := filepath.Join(r.dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) head() string {
	out, _ := r.git("rev-parse", "HEAD")
	return strings.TrimSpace(out)
}

// buildFormwork builds the binary once per run.
func buildFormwork(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "formwork")
	c := exec.Command("go", "build", "-o", bin, "./cmd/formwork")
	c.Dir = repoRoot(t)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("cannot build formwork: %v: %s", err, out)
	}
	return bin
}

// proofCorpus is the corpus every vector here runs against: the shipped
// teaching corpus, five rules across four types over real Go and SQL.
//
// It is COPIED OUT rather than checked in place because `hooks install -C
// examples/quickstart` is refused outright — that directory is a subdirectory
// of this git repository, which is the subdirectory refusal itself.
//
// Spelled here rather than read from the Makefile, deliberately. The Makefile
// comment is this repo's coverage record and this line is the code that
// comment describes; sourcing both from one place would make them agree by
// construction, and agreeing by construction is how the record came to
// advertise a corpus the proof had stopped running (#318).
const proofCorpus = "examples/quickstart"

func newRepo(t *testing.T, fw string) *repo {
	t.Helper()
	r := &repo{t: t, root: t.TempDir(), fw: fw}
	r.dir = filepath.Join(r.root, "wt")
	r.home = filepath.Join(r.root, "home")
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(r.home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The global git config the vectors run under lives in the throwaway HOME,
	// not in the work tree: a file inside the work tree is a file `git add -A`
	// commits, and the corpus this proof runs must be the corpus and nothing
	// else.
	if err := os.WriteFile(filepath.Join(r.home, ".gitconfig"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(repoRoot(t), filepath.FromSlash(proofCorpus))
	if err := os.CopyFS(r.dir, os.DirFS(src)); err != nil {
		t.Fatalf("cannot copy %s into the throwaway repository: %v", proofCorpus, err)
	}
	// Fail closed on a copy that landed nothing. With no corpus in the
	// repository every commit below fails for a reason that has nothing to do
	// with the gate, and the proof reports on that instead.
	for _, rel := range filesUnder(t, src) {
		if _, err := os.Stat(filepath.Join(r.dir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("the copy of %s did not land %s: %v", proofCorpus, rel, err)
		}
	}
	if out, code := r.git("init", "--quiet", "-b", "main", "."); code != 0 {
		t.Fatalf("git init: %s", out)
	}
	r.git("add", "-A")
	if out, code := r.git("commit", "--quiet", "-m", "quickstart corpus"); code != 0 {
		t.Fatalf("seed commit: %s", out)
	}
	// A repository that inherited a core.hooksPath from somewhere is not the
	// clean slate the install refusals below are measured against.
	if cfg, _ := r.git("config", "--get", "core.hooksPath"); strings.TrimSpace(cfg) != "" {
		t.Fatalf("the throwaway repository starts with core.hooksPath already set to %q, so the isolation this proof depends on is not in place", strings.TrimSpace(cfg))
	}
	return r
}

func TestInstalledHookRefusesAViolatingCommitAndAllowsACleanOne(t *testing.T) {
	needBinary(t, "git")
	fw := buildFormwork(t)
	r := newRepo(t, fw)

	out, code := r.fwRun("hooks", "install")
	if code != 0 {
		t.Fatalf("hooks install exited %d: %s", code, out)
	}
	if !strings.Contains(out, "pre-commit") {
		t.Errorf("install must name the lane it wired: %s", out)
	}
	cfg, _ := r.git("config", "--local", "core.hooksPath")
	if strings.TrimSpace(cfg) == "" {
		t.Error("install must set a repo-local core.hooksPath")
	}
	if out, code := r.fwRun("hooks", "verify"); code != 0 {
		t.Fatalf("verify must pass on the wiring install just wrote: exit %d: %s", code, out)
	}

	// A violating commit is refused BY THE RULE, and HEAD does not move.
	const bad = "internal/store/debug.go"
	before := r.head()
	r.write(bad, "package store\n\nimport \"fmt\"\n\nfunc DebugDump() { fmt.Println(\"debug\") }\n")
	r.git("add", bad)
	out, code = r.git("commit", "-m", "should be refused")
	if code == 0 {
		t.Fatalf("a violating commit was accepted:\n%s", out)
	}
	if !strings.Contains(out, "no-print-debugging") {
		t.Errorf("the refusal must name the rule that fired, not merely fail:\n%s", out)
	}
	if !strings.Contains(out, bad) {
		t.Errorf("the finding must name the staged file:\n%s", out)
	}
	if r.head() != before {
		t.Error("HEAD moved despite the commit being refused")
	}

	// A clean commit succeeds, and the rules demonstrably ran.
	r.git("reset", "--quiet", "HEAD", bad)
	os.Remove(filepath.Join(r.dir, bad))
	r.write("internal/store/audit.go", "package store\n\nimport \"log/slog\"\n\nfunc Audit() { slog.Info(\"ok\") }\n")
	r.git("add", "internal/store/audit.go")
	out, code = r.git("commit", "-m", "should pass")
	if code != 0 {
		t.Fatalf("a clean commit was refused:\n%s", out)
	}
	if r.head() == before {
		t.Error("HEAD did not move on a clean commit")
	}
}

// The refusals. install declines rather than taking over wiring that is
// already there, and each refusal must name its remedy — a refusal an operator
// cannot act on is a wall, not a gate.
func TestInstallRefusesRatherThanTakingOverExistingWiring(t *testing.T) {
	needBinary(t, "git")
	fw := buildFormwork(t)

	t.Run("from a subdirectory", func(t *testing.T) {
		r := newRepo(t, fw)
		sub := filepath.Join(r.dir, "nested")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		c := exec.Command(fw, "hooks", "install")
		c.Dir = sub
		c.Env = r.env()
		out, err := c.CombinedOutput()
		if err == nil {
			t.Fatalf("install from a subdirectory must be refused:\n%s", out)
		}
		if cfg, _ := r.git("config", "--local", "core.hooksPath"); strings.TrimSpace(cfg) != "" {
			t.Error("a refused install set core.hooksPath anyway")
		}
	})

	t.Run("over an existing hook", func(t *testing.T) {
		r := newRepo(t, fw)
		hook := filepath.Join(r.dir, ".git", "hooks", "pre-commit")
		if err := os.WriteFile(hook, []byte("#!/bin/sh\necho project hook ran\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		out, code := r.fwRun("hooks", "install")
		if code != 2 {
			t.Fatalf("install over a live default hook must exit 2, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "core.hooksPath") {
			t.Errorf("the refusal must say what setting core.hooksPath would switch off:\n%s", out)
		}
		if cfg, _ := r.git("config", "--local", "core.hooksPath"); strings.TrimSpace(cfg) != "" {
			t.Error("core.hooksPath was touched despite the refusal")
		}
		// And the project's own hook still runs — the refusal changed nothing.
		r.write("x.txt", "clean\n")
		r.git("add", "x.txt")
		out, _ = r.git("commit", "-m", "after refusal")
		if !strings.Contains(out, "project hook ran") {
			t.Errorf("the existing hook stopped running after a refused install:\n%s", out)
		}
	})
}

// WHAT THE PROOF RUNS, AND WHAT IT RUNS UNDER (#318).
//
// Two of the five clauses in the Makefile's hooks-e2e-proof comment were
// false. The shell this file replaced copied examples/quickstart into the
// throwaway repository and redirected HOME and XDG_CONFIG_HOME; the port kept
// the comment, wrote a two-file synthetic corpus instead — one
// forbidden-pattern rule over one .txt — and left HOME pointing at the
// developer's own. The claim mattered because the corpus IS the coverage: the
// comment sells this target as reaching "a rule YAML decoded through the type
// registry", and one rule reaches one decoder of the four the shipped corpus
// declares.
//
// So the assertions below are written against the corpus the MAKEFILE names,
// not against a corpus spelled a second time here. A comment that drifts from
// what runs fails this test rather than quietly overstating the coverage
// record, which is the failure #318 filed.

// makeCommentBlock returns one `## <target>:` comment block from the Makefile,
// unwrapped into a single line so an assertion is not hostage to where the
// prose happens to wrap. Fail-closed: a target with no comment block cannot be
// checked against what it runs, and silence about that is how the two false
// clauses survived a year.
func makeCommentBlock(t *testing.T, target string) string {
	t.Helper()
	var block []string
	in := false
	for _, line := range strings.Split(readMakefile(t), "\n") {
		switch {
		case strings.HasPrefix(line, "## "+target+":"):
			in = true
			block = append(block, strings.TrimPrefix(line, "## "))
		case in && strings.HasPrefix(line, "#"):
			block = append(block, strings.TrimSpace(strings.TrimPrefix(line, "#")))
		case in:
			return strings.Join(block, " ")
		}
	}
	t.Fatalf("the Makefile carries no `## %s:` comment block, so what that target claims to cover cannot be compared with what it runs", target)
	return ""
}

// corpusTheProofAdvertises is the corpus the hooks-e2e-proof comment says this
// proof copies into its throwaway repository.
func corpusTheProofAdvertises(t *testing.T) string {
	t.Helper()
	prose := makeCommentBlock(t, "hooks-e2e-proof")
	m := regexp.MustCompile(`copies (examples/[A-Za-z0-9._/-]+) into`).FindStringSubmatch(prose)
	if m == nil {
		t.Fatalf("the hooks-e2e-proof comment names no corpus this proof copies in, so the coverage it records is unanchored:\n%s", prose)
	}
	dir := filepath.Join(repoRoot(t), filepath.FromSlash(m[1]))
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("the hooks-e2e-proof comment names %s as the corpus it copies, and this tree has no such directory", m[1])
	}
	return dir
}

// filesUnder lists every file under dir, repo-relative and slash-spelled.
// An empty answer is fatal: an equality against nothing passes over anything.
func filesUnder(t *testing.T, dir string) []string {
	t.Helper()
	var got []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		got = append(got, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("cannot enumerate the corpus at %s: %v", dir, err)
	}
	if len(got) == 0 {
		t.Fatalf("the corpus at %s holds no files, so comparing a repository against it would assert nothing", dir)
	}
	sort.Strings(got)
	return got
}

// ruleTypesByID reads the corpus's own rule YAML for what it declares: the
// type behind every rule id. Derived rather than listed, so a rule added to
// the corpus is a rule this proof must reach.
func ruleTypesByID(t *testing.T, corpus string) map[string]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(corpus, ".formwork", "rules", "*.yaml"))
	if err != nil {
		t.Fatalf("cannot list the corpus rule files: %v", err)
	}
	idLine := regexp.MustCompile(`^\s*-\s*id:\s*(\S+)`)
	typeLine := regexp.MustCompile(`^\s*type:\s*(\S+)`)
	byID := map[string]string{}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("cannot read %s: %v", p, err)
		}
		id := ""
		for _, line := range strings.Split(string(data), "\n") {
			if m := idLine.FindStringSubmatch(line); m != nil {
				id = m[1]
				continue
			}
			if m := typeLine.FindStringSubmatch(line); m != nil && id != "" {
				byID[id] = m[1]
				id = ""
			}
		}
	}
	if len(byID) == 0 {
		t.Fatalf("no rule id/type pairs read out of %s/.formwork/rules — a coverage assertion over an empty declaration set asserts nothing", corpus)
	}
	return byID
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// The corpus, and every rule TYPE in it, reached through the installed hook.
//
// Equality, not containment: "the repository holds the corpus and some other
// things" is what the synthetic version could have claimed too. The tracked
// set is exactly what the advertised corpus ships.
//
// Then one violating commit per rule type, each refused BY ITS RULE — a
// forbidden-pattern over Go, a pair-consistency over SQL, a required-pattern
// over a script — plus a warn-severity file-size rule, which must report and
// let the commit through. Rule-type reach is the property the Makefile comment
// sells and the property the port dropped, so it is asserted per type rather
// than left to be inferred from one rule firing.
func TestInstalledHookRunsTheCorpusTheProofAdvertises(t *testing.T) {
	needBinary(t, "git")
	corpus := corpusTheProofAdvertises(t)
	fw := buildFormwork(t)
	r := newRepo(t, fw)

	out, code := r.git("ls-files")
	if code != 0 {
		t.Fatalf("git ls-files in the throwaway repository exited %d: %s", code, out)
	}
	if tracked, want := lines(out), filesUnder(t, corpus); !slices.Equal(tracked, want) {
		t.Errorf("the throwaway repository does not hold the corpus this proof advertises.\n"+
			"missing: %v\nunexpected: %v",
			missing(want, tracked), missing(tracked, want))
	}

	if out, code := r.fwRun("hooks", "install"); code != 0 {
		t.Fatalf("hooks install exited %d: %s", code, out)
	}

	declared := ruleTypesByID(t, corpus)
	reached := map[string]bool{}
	for _, v := range []struct {
		label, path, body, rule, finding string
	}{
		{
			"a forbidden pattern in Go source", "internal/store/debug.go",
			"package store\n\nimport \"fmt\"\n\nfunc DebugDump() { fmt.Println(\"debug\") }\n",
			"no-print-debugging", "forbidden pattern matched",
		},
		{
			"a migration that alters without a transaction", "db/migrations/0002_add_note.sql",
			"ALTER TABLE orders ADD COLUMN note TEXT;\n",
			"migrations-are-transactional", "required companion",
		},
		{
			"a script with no usage line", "scripts/rollback.go",
			"package main\n\nfunc main() { println(\"rolling back\") }\n",
			"cli-usage-guard", "required pattern missing",
		},
	} {
		t.Run(v.label, func(t *testing.T) {
			before := r.head()
			r.write(v.path, v.body)
			r.git("add", v.path)
			out, code := r.git("commit", "-m", "should be refused: "+v.label)
			defer func() {
				r.git("reset", "--quiet", "--hard", before)
				os.Remove(filepath.Join(r.dir, v.path))
			}()
			if code == 0 {
				t.Fatalf("a violating commit was accepted:\n%s", out)
			}
			if !strings.Contains(out, v.rule) {
				t.Errorf("the refusal must name the rule that fired, not merely fail:\n%s", out)
			} else {
				// Marked reached only where the rule itself is named. A vector
				// that failed, or that was refused for some other reason, must
				// not count as coverage of its type.
				reached[declared[v.rule]] = true
			}
			if !strings.Contains(out, v.finding) {
				t.Errorf("the refusal must carry the finding %q the rule reports:\n%s", v.finding, out)
			}
			if !strings.Contains(out, v.path) {
				t.Errorf("the finding must name the staged file:\n%s", out)
			}
			if r.head() != before {
				t.Error("HEAD moved despite the commit being refused")
			}
			// Every rule in the corpus decoded and ran, not merely the one
			// that fired: the hook's own output names each of them.
			for id := range declared {
				if !strings.Contains(out, "["+id+"]") {
					t.Errorf("rule %s is in the corpus and the hook's run never names it — the corpus reached the registry only in part:\n%s", id, out)
				}
			}
			reached[declared[v.rule]] = true
		})
	}

	// A warn-severity rule reports through the hook and lets the commit land.
	// The accept direction, and the severity contract, in one vector.
	t.Run("a handler over the warn cap", func(t *testing.T) {
		before := r.head()
		r.write("internal/handler/totals.go",
			"package handler\n\nfunc Totals() int {\n\tn := 0\n"+strings.Repeat("\tn++\n", 45)+"\treturn n\n}\n")
		r.git("add", "internal/handler/totals.go")
		out, code := r.git("commit", "-m", "warns but lands")
		if code != 0 {
			t.Fatalf("a commit tripping only a warn-severity rule was refused:\n%s", out)
		}
		if !strings.Contains(out, "handlers-stay-small") || !strings.Contains(out, "WARN") {
			t.Errorf("the warning must reach the operator through the hook:\n%s", out)
		} else {
			reached[declared["handlers-stay-small"]] = true
		}
		if r.head() == before {
			t.Error("HEAD did not move on a commit that only warns")
		}
	})

	for id, typ := range declared {
		if !reached[typ] {
			t.Errorf("rule type %q (%s) is declared by the corpus and no commit vector reaches it through the hook — "+
				"the type registry coverage this target records is smaller than the corpus it runs", typ, id)
		}
	}
}

// missing returns the members of want that are absent from got.
func missing(want, got []string) []string {
	var out []string
	for _, w := range want {
		if !slices.Contains(got, w) {
			out = append(out, w)
		}
	}
	return out
}

// The isolation half of the same claim. GIT_CONFIG_GLOBAL covers ~/.gitconfig
// and nothing else: git reads its global EXCLUDES file from
// $XDG_CONFIG_HOME/git/ignore, falling back to $HOME/.config/git/ignore, so
// with HOME left at the developer's own whatever they ignore globally decides
// what `git add -A` puts into this proof's corpus. A machine ignoring *.txt
// would have quietly emptied the pre-port corpus of its fixtures.
//
// Asserted by planting an ignore under the throwaway HOME and requiring git to
// honour it, which no amount of env plumbing can fake: git honours that file
// only if it is reading the throwaway HOME rather than the real one. The
// second file is the control — a probe that ignored EVERYTHING would satisfy
// the first assertion on its own.
func TestInstallProofsRunUnderAThrowawayHomeGitReads(t *testing.T) {
	needBinary(t, "git")
	fw := buildFormwork(t)
	r := newRepo(t, fw)

	const ignored = "ignored-by-the-throwaway-home.txt"
	const kept = "seen-by-git-anyway.txt"
	if err := os.MkdirAll(filepath.Join(r.home, ".config", "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.home, ".config", "git", "ignore"), []byte(ignored+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.write(ignored, "planted\n")
	r.write(kept, "planted\n")
	if out, code := r.git("add", "-A"); code != 0 {
		t.Fatalf("git add exited %d: %s", code, out)
	}
	out, code := r.git("ls-files")
	if code != 0 {
		t.Fatalf("git ls-files exited %d: %s", code, out)
	}
	tracked := lines(out)
	if slices.Contains(tracked, ignored) {
		t.Errorf("git did not honour the ignore file planted under the throwaway HOME at %s — "+
			"it is reading some other home, so this proof runs under whatever the machine's own git config says:\n%s",
			r.home, out)
	}
	if !slices.Contains(tracked, kept) {
		t.Errorf("git tracked neither planted file, so the assertion above passed over a repository git could not see:\n%s", out)
	}
}
