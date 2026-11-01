package cli

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
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
