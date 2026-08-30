// Command assert-trusted-stamp asserts the GoReleaser-built binary in dist/ is
// version-stamped and TRUSTED. Shared by ci.yml (snapshot, every PR) and
// release.yml (the real tagged build, before anything is published) — one
// program, two callers, so the two workflows cannot drift apart.
//
// WHY THIS EXISTS. A release-breaking regression shipped undetected on
// 2026-07-23: isReleaseVersion wrongly required a leading "v", but GoReleaser's
// {{ .Version }} stamps the tag with "v" stripped. Every released binary
// resolved to a STAMPED-BUT-UNTRUSTED string — never the bare "dev" sentinel,
// so a "not dev" check passed — while still being annotated "(unreleased
// build)" and failing every engine: constraint.
//
// So it asserts the reported version matches what GoReleaser actually stamped
// (dist/metadata.json) and carries no unreleased annotation. There is no
// separate `ver == "dev"` guard: the exact-match check already exits for any
// version that is not the stamped one, and the stamped one is never "dev".
//
// Replaces .github/scripts/assert-trusted-stamp.sh.
package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func fail(format string, a ...any) {
	fmt.Printf("::error::"+format+"\n", a...)
	os.Exit(1)
}

// distDir is where GoReleaser writes the build both callers assert against.
const distDir = "dist"

// releaseBinaryPath finds the linux_amd64 formwork binary GoReleaser wrote
// under dir, whose contents reach it as fsys, and returns the path to run.
// The error is the reason there is nothing to assert against, phrased for the
// ::error:: line the workflows print.
//
// It has two shapes and they stay apart (#267.7). "The build produced
// nothing" sends a reader to the build; "this program could not read the
// tree" sends them to the runner, and reporting the second as the first sends
// them hunting a bug in a build that worked.
func releaseBinaryPath(fsys fs.FS, dir string) (string, error) {
	var bin string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		// The walker's own failure, surfaced rather than absorbed: a
		// directory this program cannot read is not one it can clear,
		// and a search that skipped part of the tree has not searched
		// it. d is nil here, so nothing below may be reached.
		if err != nil {
			return err
		}
		if d.IsDir() || bin != "" {
			return nil
		}
		if d.Name() == "formwork" && strings.Contains(p, "linux_amd64") {
			bin = p
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("cannot search %s/ for the release binary: %w", dir, err)
	}
	if bin == "" {
		return "", fmt.Errorf("no linux_amd64 formwork binary found under %s/ — nothing to assert against", dir)
	}
	return filepath.Join(dir, bin), nil
}

func main() {
	bin, err := releaseBinaryPath(os.DirFS(distDir), distDir)
	if err != nil {
		fail("%v", err)
	}

	raw, err := exec.Command(bin, "version").Output()
	if err != nil {
		fail("could not run %s version: %v", bin, err)
	}
	out := strings.TrimSpace(string(raw))
	fmt.Println("version output:", out)
	ver := strings.TrimPrefix(out, "formwork ")

	meta, err := os.ReadFile(filepath.Join(distDir, "metadata.json"))
	if err != nil {
		fail("cannot read %s/metadata.json: %v", distDir, err)
	}
	var m struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(meta, &m); err != nil {
		fail("%s/metadata.json does not parse: %v", distDir, err)
	}
	fmt.Println("goreleaser-stamped version:", m.Version)

	if ver != m.Version {
		fail("release build version mismatch — binary reports %q, goreleaser stamped %q; "+
			"the ldflags target has drifted from "+
			"github.com/buildfoundry-nz/formwork/internal/cli.version", ver, m.Version)
	}
	if strings.Contains(out, "(unreleased build)") {
		fail("release build reported as unreleased — binary version %q was not recognised as "+
			"trusted; check internal/cli/version.go isReleaseVersion", ver)
	}
}
