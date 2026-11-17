package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCheckFileHandlerCompilesOnlyChangedCorpora(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".formwork", "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".formwork", "formwork.yaml"), []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rulesPath := filepath.Join(root, ".formwork", "rules", "rules.yaml")
	writeRules := func(pattern string, modTime time.Time) {
		t.Helper()
		body := "rules:\n  - id: no-todo\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: " + pattern + "}\n"
		if err := os.WriteFile(rulesPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(rulesPath, modTime, modTime); err != nil {
			t.Fatal(err)
		}
	}
	fixedTime := time.Unix(1_700_000_000, 0)
	writeRules("TODO", fixedTime)
	loads := 0
	loadHook = func() { loads++ }
	defer func() { loadHook = nil }()
	corpus, err := newCheckFileCorpus(root)
	if err != nil {
		t.Fatal(err)
	}
	defer corpus.Close()
	var handler mcp.ToolHandlerFor[checkFileInput, any] = corpus.handler()
	content := "TOOD\n"
	call := func() checkContentDocument {
		t.Helper()
		_, output, err := handler(context.Background(), nil, checkFileInput{Path: "a.txt", Content: &content})
		if err != nil {
			t.Fatal(err)
		}
		doc, ok := output.(checkContentDocument)
		if !ok {
			t.Fatalf("check_file output type = %T", output)
		}
		return doc
	}
	if doc := call(); doc.Status != "clean" {
		t.Fatalf("initial rule unexpectedly fired: %+v", doc)
	}
	if doc := call(); doc.Status != "clean" {
		t.Fatalf("unchanged rule unexpectedly fired: %+v", doc)
	}
	if loads != 1 {
		t.Fatalf("identical calls compiled the corpus %d times, want 1", loads)
	}
	writeRules("TOOD", fixedTime)
	deadline := time.Now().Add(5 * time.Second)
	for {
		doc := call()
		if doc.Status == "findings" && doc.FindingsTotal == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("same-size, same-mtime rule edit stayed stale: %+v", doc)
		}
		time.Sleep(time.Millisecond)
	}
	if loads != 2 {
		t.Fatalf("changed corpus compiled %d times, want 2", loads)
	}

	content = "NEW\n"
	extraPath := filepath.Join(root, ".formwork", "rules", "extra.yaml")
	if err := os.WriteFile(extraPath, []byte("rules:\n  - id: new-rule\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: NEW}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		doc := call()
		if doc.Status == "findings" && doc.FindingsTotal == 1 && doc.Findings[0].Rule == "new-rule" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("created rule file did not invalidate the corpus: %+v", doc)
		}
		time.Sleep(time.Millisecond)
	}
	if loads != 3 {
		t.Fatalf("created rule file produced %d corpus loads, want 3", loads)
	}
	if err := os.Remove(extraPath); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		doc := call()
		if doc.Status == "clean" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("deleted rule file did not invalidate the corpus: %+v", doc)
		}
		time.Sleep(time.Millisecond)
	}
	if loads != 4 {
		t.Fatalf("deleted rule file produced %d corpus loads, want 4", loads)
	}
	corpus.reliable.Store(false)
	call()
	call()
	if loads != 6 {
		t.Fatalf("watcher failure reused the corpus: got %d loads, want 6", loads)
	}
}
