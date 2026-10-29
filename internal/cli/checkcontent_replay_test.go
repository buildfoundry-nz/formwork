package cli

import (
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
)

func TestCheckContentReplayHandleIncludesNotScannedAttribution(t *testing.T) {
	cfg := &config.Config{CorpusHash: "sha256:test"}
	started := time.Unix(0, 0)
	states := []*notScannedOut{
		nil,
		{By: "non-regular"},
		{By: "scan.ignore", Glob: "ignored/**"},
		{By: "scan.ignore", Glob: "other/**"},
		{By: "scan.gitignore", Rule: ".gitignore:1:ghost.txt"},
		{By: "scan.gitignore", Rule: ".gitignore:1:gh*.txt"},
	}
	seen := map[string]*notScannedOut{}
	for _, state := range states {
		handle := newCheckContentDocument(cfg, "a.txt", []byte("clean\n"), started, state).ReplayHandle
		if previous, exists := seen[handle]; exists {
			t.Fatalf("distinct reachability states share handle %q: %+v and %+v", handle, previous, state)
		}
		seen[handle] = state
		again := newCheckContentDocument(cfg, "a.txt", []byte("clean\n"), started, state).ReplayHandle
		if again != handle {
			t.Fatalf("identical state %+v changed handle: %q then %q", state, handle, again)
		}
	}
}
