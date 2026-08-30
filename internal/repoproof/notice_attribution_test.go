// notice_attribution_test.go — NOTICE attributes every third-party module the
// released binary statically links, and reproduces every copyright notice
// those modules' own licence files carry.
//
// WHY THIS EXISTS. NOTICE opens by asserting "The following attributions are
// reproduced as their licenses require." Nothing in the repository checked
// that. Six modules were linked into all four released tarballs — doublestar,
// regexp2, Masterminds/semver, wazero-helpers, golang.org/x/sys and
// google.golang.org/protobuf — with no attribution at all, four of them under
// MIT ("The above copyright notice ... shall be included in all copies") and
// two under BSD-3 ("Redistributions in binary form must reproduce the above
// copyright notice"). An attribution file that claims completeness it does not
// have is worse than none, and this repository is about to be public.
//
// The drift that produced it is structural: adding a dependency is one line in
// go.mod and nothing anywhere connects that line to NOTICE. So the guard asks
// the toolchain, not a human — `go list -deps` over the exact build matrix
// .goreleaser.yaml ships, reduced to modules, compared against what NOTICE
// declares, in BOTH directions. The next dependency added fails this test
// until it is attributed, and an attribution left behind by a removed
// dependency fails it too.
//
// It is not enough to name the module. A module name with no copyright line
// satisfies nothing any licence asks for, so the second test reads each
// module's own LICENSE/NOTICE/ATTRIB out of the module cache and requires
// every copyright notice in them to appear in NOTICE verbatim. That is what
// makes this an attribution check rather than an inventory check.
//
// THIS IS A TEST, NOT A FORMWORK RULE, deliberately. A rule reads tracked file
// CONTENT; the linked-module set is not in the tree at all — it is the output
// of the linker's own dependency resolution across four platforms, and the
// copyright notices live in the module cache. Neither is expressible as a
// pattern over tracked files.
package repoproof_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// releaseTarget is one row of .goreleaser.yaml's build matrix. The guard is
// about what SHIPS, so it asks about every platform that ships rather than
// whichever one the test happens to run on — a dependency pulled in only on
// linux/arm64 is still statically linked into a released tarball.
type releaseTarget struct{ goos, goarch string }

var releaseTargets = []releaseTarget{
	{"linux", "amd64"},
	{"linux", "arm64"},
	{"darwin", "amd64"},
	{"darwin", "arm64"},
}

const formworkModule = "github.com/buildfoundry-nz/formwork"

// noticeDeclaration matches the opening line of an attribution block: a module
// path at column zero followed by " (" and the licence name. The four blocks
// NOTICE already carried used that shape; this pins it. Anchoring at column
// zero is what keeps a module path quoted inside a reproduced notice — those
// are indented — from reading as a declaration.
var noticeDeclaration = regexp.MustCompile(`^([a-z0-9][a-z0-9-]*(?:\.[a-z0-9-]+)+(?:/[^\s()]+)+) \(`)

// copyrightNotice matches a copyright line in a licence file. "Portions
// Copyright" is the PostgreSQL and BSD-derived form and carries the same
// obligation as the plain one.
var copyrightNotice = regexp.MustCompile(`^[ \t]*((?:Portions )?Copyright[ \t].*?)[ \t]*$`)

// licenceFileName matches the files a module uses to state its terms. The
// spread is real: LICENSE, LICENSE.txt, NOTICE, NOTICE.txt, LICENSE.POSTGRESQL,
// COPYRIGHT and regexp2's ATTRIB, which is where its .NET-derived and
// Go-derived copyrights live — the LICENSE alone would attribute only Doug
// Clark and miss Microsoft Corporation and the Go Authors entirely.
var licenceFileName = regexp.MustCompile(`(?i)^(licen[sc]e|notice|copying|copyright|attrib)([._-].*)?$`)

// linkedModules returns every third-party module whose code is compiled into
// the released binary, mapped to the module's directory in the module cache.
func linkedModules(t *testing.T) map[string]string {
	t.Helper()
	needBinary(t, "go")
	root := repoRoot(t)
	mods := map[string]string{}
	for _, target := range releaseTargets {
		cmd := exec.Command("go", "list", "-deps",
			"-f", "{{with .Module}}{{.Path}}\t{{.Dir}}{{end}}", "./cmd/formwork")
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"CGO_ENABLED=0", "GOOS="+target.goos, "GOARCH="+target.goarch)
		out, err := cmd.Output()
		if err != nil {
			var stderr string
			if ee, ok := err.(*exec.ExitError); ok {
				stderr = string(ee.Stderr)
			}
			t.Fatalf("cannot list %s/%s dependencies of ./cmd/formwork: %v\n%s",
				target.goos, target.goarch, err, stderr)
		}
		for _, line := range strings.Split(string(out), "\n") {
			path, dir, ok := strings.Cut(strings.TrimSpace(line), "\t")
			if !ok || path == "" || path == formworkModule {
				continue
			}
			if dir == "" {
				t.Fatalf("module %s has no directory on disk, so its licence cannot be read", path)
			}
			mods[path] = dir
		}
	}
	if len(mods) == 0 {
		t.Fatal("no third-party modules resolved — the dependency list is empty, which cannot be right")
	}
	return mods
}

func noticeText(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "NOTICE"))
	if err != nil {
		t.Fatalf("cannot read NOTICE: %v", err)
	}
	return string(b)
}

// noticeDeclaredModules returns the module paths NOTICE opens an attribution
// block for.
func noticeDeclaredModules(t *testing.T) map[string]bool {
	t.Helper()
	declared := map[string]bool{}
	for _, line := range strings.Split(noticeText(t), "\n") {
		if m := noticeDeclaration.FindStringSubmatch(line); m != nil {
			declared[m[1]] = true
		}
	}
	return declared
}

// moduleCopyrightNotices reads every copyright line out of one module's own
// licence files.
func moduleCopyrightNotices(t *testing.T, dir string) map[string][]string {
	t.Helper()
	byFile := map[string][]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !licenceFileName.MatchString(d.Name()) {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			rel = path
		}
		for _, line := range strings.Split(string(b), "\n") {
			if m := copyrightNotice.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
				byFile[rel] = append(byFile[rel], m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("cannot read licence files under %s: %v", dir, err)
	}
	return byFile
}

// flatten renders text as a single whitespace-normalised line so a notice that
// NOTICE wraps or indents still matches word for word. Words and their order
// still have to be exact; only the whitespace between them is free.
var whitespaceRun = regexp.MustCompile(`\s+`)

func flatten(s string) string {
	return strings.TrimSpace(whitespaceRun.ReplaceAllString(s, " "))
}

func sortedStringKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestNoticeAttributesEveryStaticallyLinkedModule(t *testing.T) {
	linked := linkedModules(t)
	declared := noticeDeclaredModules(t)

	var missing []string
	for _, path := range sortedStringKeys(linked) {
		if !declared[path] {
			missing = append(missing, path)
		}
	}
	if len(missing) > 0 {
		t.Errorf("NOTICE claims the attributions its dependencies' licences require are reproduced, "+
			"but %d module(s) linked into the released binary have no attribution block:\n  %s\n"+
			"Every one ships inside all four tarballs. Add a block opening `<module path> (<licence>)` "+
			"at column zero — there is no allowlist here.",
			len(missing), strings.Join(missing, "\n  "))
	}

	var stale []string
	for path := range declared {
		if _, ok := linked[path]; !ok {
			stale = append(stale, path)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("NOTICE attributes %d module(s) the released binary does not link:\n  %s\n"+
			"An attribution for software that is not shipped is a false claim in the other direction.",
			len(stale), strings.Join(stale, "\n  "))
	}
}

func TestNoticeReproducesEveryDependencyCopyrightNotice(t *testing.T) {
	notice := flatten(noticeText(t))
	linked := linkedModules(t)

	for _, path := range sortedStringKeys(linked) {
		byFile := moduleCopyrightNotices(t, linked[path])
		if len(byFile) == 0 {
			t.Errorf("%s ships no licence file carrying a copyright notice, "+
				"so its terms cannot be checked", path)
			continue
		}
		var files []string
		for f := range byFile {
			files = append(files, f)
		}
		sort.Strings(files)
		for _, f := range files {
			for _, line := range byFile[f] {
				if !strings.Contains(notice, flatten(line)) {
					t.Errorf("NOTICE does not reproduce a copyright notice %s carries in its %s:\n  %s",
						path, f, line)
				}
			}
		}
	}
}
