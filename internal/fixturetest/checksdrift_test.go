// checksdrift_test.go — the N copies of one rule's checker are ONE checker.
//
// WHAT IS BROKEN WITHOUT THIS. A `command` rule reached through the escape
// hatch runs `go run checks/check.go .` with the fixture directory as its
// working directory, so the detector is not a file the rule points at: it is a
// file every fixture tree of that rule carries its own copy of. Two copies was
// already a duplication nothing held; #262's fix took
// schema-fk-delete-path-indexed to FIVE, one per fire-*/pass-* tree, and each
// of the three new trees exists to prove that one arm of that checker refuses
// what it used to pass.
//
// A copy that drifts does not fail. It reports. Edit the checker in fire-2 and
// leave pass-1 behind and both trees still run, each against its own detector,
// and `formwork test` prints `OK — 5 fixture(s)`: the pass tree is then
// vouching for code the fire trees do not contain, and every fire tree is
// proving an arm of a program the repository does not otherwise ship. The
// failure mode is the one this repository keeps finding — a proof that reads
// green while asserting nothing about the thing under test — and it arrives
// through an ordinary edit rather than through anybody's mistake about the
// rule.
//
// WHY A GO TEST AND NOT A FORMWORK RULE. internal/scan prunes any directory
// named `.formwork` at ANY depth, unconditionally, and that prune is declarable
// in no rule's scope (internal/scan/skipdirs.go). Every path this asserts over
// lives under `examples/<corpus>/.formwork/fixtures/`, so no rule in this
// repository can read one of them — the same structural gap this repository's
// own vocabulary rule records for itself, in its header, as the reason it
// cannot see the corpus where a ported rule is actually written. A rule here
// would be a lockdown over an empty file set, which is the shape it exists to
// refuse.
//
// WHAT IT ASSERTS. Within ONE rule's fixture tree, every fire-*/pass-*
// directory that carries a `checks/` tree carries the SAME set of relative
// paths under it, with the same bytes. A fixture directory with no `checks/`
// tree at all is not judged: that is a fixture which does not use the escape
// hatch, not a copy that went missing.
//
// It is not vacuous and it is green on landing, both measured rather than
// assumed: at #262 seven rules in examples/palletra-port-full carried a
// `checks/` tree, seventeen copies between them, and drift across them was
// zero. Those two figures are a reading taken on the day and not a claim this
// file holds — every run prints the live count, and what IS asserted is that
// the count is not zero: a run that finds no `checks/` tree has lost its
// subject and fails rather than passing over nothing.
package fixturetest_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// moduleRoot walks up to the directory holding go.mod. Failing closed rather
// than guessing, for the same reason corpusRoot does: a proof that cannot find
// the tree it judges must not report a pass over the tree it did find.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot read working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory — cannot locate the repo root")
		}
		dir = parent
	}
}

// checksFiles returns every file under fixtureDir/checks keyed by its path
// relative to that `checks` directory, valued by the SHA-256 of its bytes.
// Nil (not an empty map) means the fixture tree carries no checks/ at all,
// which is the case the caller leaves alone.
func checksFiles(t *testing.T, fixtureDir string) map[string]string {
	t.Helper()
	checks := filepath.Join(fixtureDir, "checks")
	info, err := os.Stat(checks)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("%s: %v", checks, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s: expected a directory of checker source, found a %s", checks, info.Mode())
	}
	out := map[string]string{}
	err = filepath.WalkDir(checks, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(checks, path)
		if relErr != nil {
			return relErr
		}
		sum := sha256.Sum256(data)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", checks, err)
	}
	if len(out) == 0 {
		t.Fatalf("%s exists but holds no file — a rule whose detector directory is empty runs no detector", checks)
	}
	return out
}

func TestFixtureCheckerCopiesDoNotDrift(t *testing.T) {
	root := moduleRoot(t)
	corpora, err := filepath.Glob(filepath.Join(root, "examples", "*", ".formwork", "fixtures"))
	if err != nil {
		t.Fatalf("globbing the corpora: %v", err)
	}
	if len(corpora) == 0 {
		t.Fatalf("no examples/*/.formwork/fixtures under %s — this proof has no subject", root)
	}

	rulesWithChecks, copies := 0, 0
	for _, fixturesRoot := range corpora {
		ruleDirs, err := os.ReadDir(fixturesRoot)
		if err != nil {
			t.Fatalf("reading %s: %v", fixturesRoot, err)
		}
		for _, rd := range ruleDirs {
			if !rd.IsDir() {
				continue
			}
			ruleDir := filepath.Join(fixturesRoot, rd.Name())
			fixtures, err := os.ReadDir(ruleDir)
			if err != nil {
				t.Fatalf("reading %s: %v", ruleDir, err)
			}

			// carriers is every fixture tree of THIS rule that uses the escape
			// hatch, in read order, so the report below names them the way the
			// directory listing does.
			var carriers []string
			byFixture := map[string]map[string]string{}
			for _, f := range fixtures {
				if !f.IsDir() {
					continue
				}
				if !strings.HasPrefix(f.Name(), "fire-") && !strings.HasPrefix(f.Name(), "pass-") {
					continue
				}
				files := checksFiles(t, filepath.Join(ruleDir, f.Name()))
				if files == nil {
					continue
				}
				carriers = append(carriers, f.Name())
				byFixture[f.Name()] = files
				copies += len(files)
			}
			if len(carriers) == 0 {
				continue
			}
			sort.Strings(carriers)
			rulesWithChecks++

			// The union of relative paths, so a copy that LOST a file is
			// reported by the same pass as a copy whose bytes moved: absent is
			// drift to nothing, and it is the shape a `git mv` produces.
			pathSet := map[string]bool{}
			for _, files := range byFixture {
				for rel := range files {
					pathSet[rel] = true
				}
			}
			rels := make([]string, 0, len(pathSet))
			for rel := range pathSet {
				rels = append(rels, rel)
			}
			sort.Strings(rels)

			for _, rel := range rels {
				// Grouped by content so the failure says WHICH trees agree
				// with which — with five copies, "they differ" does not tell
				// an author which one they forgot.
				groups := map[string][]string{}
				for _, name := range carriers {
					sum, ok := byFixture[name][rel]
					if !ok {
						sum = "absent"
					}
					groups[sum] = append(groups[sum], name)
				}
				if len(groups) == 1 {
					continue
				}
				sums := make([]string, 0, len(groups))
				for sum := range groups {
					sums = append(sums, sum)
				}
				sort.Strings(sums)
				var lines []string
				for _, sum := range sums {
					short := sum
					if short != "absent" {
						short = short[:12]
					}
					lines = append(lines, "    "+short+"  "+strings.Join(groups[sum], ", "))
				}
				t.Errorf("%s: checks/%s is not one file across the fixture trees that carry it.\n"+
					"  Each tree runs its OWN copy — the rule's argv is `checks/check.go` resolved against the fixture directory — so a drifted copy means the pass tree vouches for code the fire trees do not contain, and every fire tree proves an arm of a program this repository does not otherwise ship. `formwork test` reports OK throughout.\n"+
					"  Groups (sha256 prefix, then the trees carrying it):\n%s",
					filepath.Base(ruleDir), rel, strings.Join(lines, "\n"))
			}
		}
	}

	if rulesWithChecks == 0 {
		t.Fatal("no rule in any corpus carries a checks/ tree — this proof judged nothing; if the command escape hatch has been retired, retire this test with it rather than leaving it green over an empty set")
	}
	t.Logf("judged %d checker cop(ies) across %d rule(s) carrying a checks/ tree", copies, rulesWithChecks)
}
