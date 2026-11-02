package cli

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/meta"
)

func TestCheckContentBatchLoadsConfigOnce(t *testing.T) {
	original := checkContentLoadGated
	calls := 0
	checkContentLoadGated = func(root string, stderr io.Writer) (*config.Config, bool) {
		calls++
		return original(root, stderr)
	}
	defer func() { checkContentLoadGated = original }()

	input := "{\"path\":\"src/notes.txt\",\"content\":\"clean\\n\"}\n" +
		"{\"path\":\"src/other.txt\",\"content\":\"also clean\\n\"}\n"
	var stdout, stderr bytes.Buffer
	code := runCheckContent([]string{"-C", filepath.Join("testdata", "toyrepo"), "--batch"}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("batch exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	if calls != 1 {
		t.Fatalf("config loaded %d times for two records, want exactly once", calls)
	}
}

// The gitignore snapshot is a property of the worktree, not of a record: a
// batch asks git for it once. Asking per record re-ran a worktree-wide
// `git ls-files --others --ignored` for every staged file, which put a
// 150-file commit's checkpoint at 12 scans and a 1,779-file merge past 40
// minutes on an ~80k-file worktree.
func TestCheckContentBatchResolvesGitIgnoreOnce(t *testing.T) {
	original := checkContentResolveGitIgnore
	calls := 0
	checkContentResolveGitIgnore = func(cfg *config.Config, root string) meta.GitIgnoreResult {
		calls++
		return original(cfg, root)
	}
	defer func() { checkContentResolveGitIgnore = original }()

	input := "{\"path\":\"src/notes.txt\",\"content\":\"clean\\n\"}\n" +
		"{\"path\":\"src/other.txt\",\"content\":\"also clean\\n\"}\n" +
		"{\"path\":\"src/third.txt\",\"content\":\"still clean\\n\"}\n"
	var stdout, stderr bytes.Buffer
	code := runCheckContent([]string{"-C", filepath.Join("testdata", "toyrepo"), "--batch"}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("batch exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	if calls != 1 {
		t.Fatalf("gitignore snapshot resolved %d times for three records, want exactly once", calls)
	}
}
