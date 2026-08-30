// main_test.go — the search for the binary to assert against. Everything
// after it (the version comparison, the unreleased-annotation check) runs a
// real GoReleaser build and belongs to the workflows; the search is the part
// that decides whether those assertions run at all, and it is the part that
// can decide wrongly in silence.
package main

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// distTree is dist/ as GoReleaser leaves it: one directory per target, the
// metadata beside them, and the binary this program asserts against down the
// linux_amd64 one.
func distTree() fstest.MapFS {
	return fstest.MapFS{
		"metadata.json":                    {Data: []byte(`{"version":"1.2.3"}`)},
		"formwork_linux_amd64_v1/formwork": {Data: []byte("ELF")},
		"formwork_darwin_arm64/formwork":   {Data: []byte("MACHO")},
	}
}

func TestReleaseBinaryPathFindsTheLinuxBuild(t *testing.T) {
	got, err := releaseBinaryPath(distTree(), "dist")
	if err != nil {
		t.Fatal(err)
	}
	if got != "dist/formwork_linux_amd64_v1/formwork" {
		t.Fatalf("wrong binary: %q", got)
	}
}

func TestReleaseBinaryPathReportsAnEmptyTree(t *testing.T) {
	only := fstest.MapFS{"formwork_darwin_arm64/formwork": {Data: []byte("MACHO")}}
	_, err := releaseBinaryPath(only, "dist")
	if err == nil {
		t.Fatal("a tree with no linux_amd64 build has nothing to assert against")
	}
	if !strings.Contains(err.Error(), "nothing to assert against") {
		t.Fatalf("a build that produced nothing must say so: %v", err)
	}
}

// unreadableFS is dist/ with one directory the walker cannot read: a
// permission-denied target directory, a mount that went away mid-job, an
// archive half-extracted. WalkDir hands that failure to the callback, which
// is the only place it can be seen.
type unreadableFS struct {
	fs.FS
	dir string
	err error
}

func (u unreadableFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == u.dir {
		return nil, u.err
	}
	return fs.ReadDir(u.FS, name)
}

// A directory the search cannot read is not a directory it can clear. The
// shell original ran under `set -euo pipefail` and aborted on it; the Go port
// returns nil from the callback, so the walk finishes, the failure is gone,
// and whatever the search did or did not find stands as the answer.
//
// The two outcomes must stay distinguishable. "The build produced nothing"
// sends a reader to the build; "this program could not read the tree" sends
// them to the runner, and reporting the second as the first sends them to
// look for a bug in a build that worked.
func TestReleaseBinaryPathRefusesATreeItCannotRead(t *testing.T) {
	boom := errors.New("permission denied")
	blocked := unreadableFS{FS: distTree(), dir: "formwork_linux_amd64_v1", err: boom}
	got, err := releaseBinaryPath(blocked, "dist")
	if err == nil {
		t.Fatalf("an unreadable dist/ was cleared, returning %q", got)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("the read failure must survive to the ::error:: line; got %v", err)
	}
	if strings.Contains(err.Error(), "nothing to assert against") {
		t.Fatalf("a tree that could not be read is not a build that produced nothing; got %v", err)
	}
}
