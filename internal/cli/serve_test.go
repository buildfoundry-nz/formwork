package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/cli"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type checkFileTestArgs struct {
	Path    string  `json:"path"`
	Content *string `json:"content,omitempty"`
}

func TestFormworkMCPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_FORMWORK_MCP_HELPER") != "1" {
		return
	}
	separator := 0
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator == 0 {
		os.Exit(2)
	}
	os.Exit(cli.Run(os.Args[separator+1:], os.Stdin, os.Stdout, os.Stderr))
}

func connectFormworkMCP(t *testing.T, root string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	cmd := exec.Command(os.Args[0], "-test.run=^TestFormworkMCPHelperProcess$", "--", "serve", "--mcp", "-C", root)
	cmd.Env = append(os.Environ(), "GO_WANT_FORMWORK_MCP_HELPER=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "formwork-test-client", Version: "v1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect MCP: %v; stderr: %s", err, stderr.String())
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("close MCP session: %v; stderr: %s", err, stderr.String())
		}
	})
	return session
}

func callCheckFile(t *testing.T, session *mcp.ClientSession, args checkFileTestArgs) (contentDocument, *mcp.CallToolResult) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "check_file", Arguments: args})
	if err != nil {
		t.Fatalf("call check_file: %v", err)
	}
	if result.IsError {
		return contentDocument{}, result
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured check_file result: %v", err)
	}
	var doc contentDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode structured check_file result: %v; raw: %s", err, raw)
	}
	return doc, result
}

func TestServeMCPCheckFileUsesSuppliedAndDiskContent(t *testing.T) {
	root := contentRepo(t, "", `rules:
  - id: no-todo
    type: forbidden-pattern
    scope: {include: ['**/*.txt']}
    params: {pattern: TODO}
    cure: resolve the item
`)
	mustWrite(t, filepath.Join(root, "src", "note.txt"), "TODO from disk\n")
	session := connectFormworkMCP(t, root)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "check_file" || tools.Tools[0].InputSchema == nil || tools.Tools[0].OutputSchema != nil {
		t.Fatalf("tools/list = %+v", tools.Tools)
	}

	unsaved := "TODO from unsaved\n"
	doc, result := callCheckFile(t, session, checkFileTestArgs{Path: "src/note.txt", Content: &unsaved})
	if result.IsError || doc.Status != "findings" || doc.FindingsTotal != 1 || doc.Findings[0].Cure != "resolve the item" {
		t.Fatalf("unsaved result = %+v; MCP result = %+v", doc, result)
	}
	if len(result.Content) != 1 || len(result.Content[0].(*mcp.TextContent).Text) > 1024 {
		t.Fatalf("check_file duplicated the full report into text content: %+v", result.Content)
	}

	empty := ""
	doc, result = callCheckFile(t, session, checkFileTestArgs{Path: "src/note.txt", Content: &empty})
	if result.IsError || doc.Status != "clean" || doc.FindingsTotal != 0 {
		t.Fatalf("empty supplied content must override disk: doc = %+v; MCP result = %+v", doc, result)
	}

	doc, result = callCheckFile(t, session, checkFileTestArgs{Path: "src/note.txt"})
	if result.IsError || doc.Status != "findings" || doc.FindingsTotal != 1 {
		t.Fatalf("omitted content must read disk: doc = %+v; MCP result = %+v", doc, result)
	}
}

func TestServeMCPCheckFileReloadsChangedRules(t *testing.T) {
	root := contentRepo(t, "", `rules:
  - id: no-todo
    type: forbidden-pattern
    scope: {include: ['**']}
    params: {pattern: TODO}
`)
	session := connectFormworkMCP(t, root)
	content := "BROKEN\n"
	doc, _ := callCheckFile(t, session, checkFileTestArgs{Path: "a.txt", Content: &content})
	if doc.Status != "clean" {
		t.Fatalf("initial rules unexpectedly fired: %+v", doc)
	}
	replacement := filepath.Join(root, ".formwork", "rules", "rules.tmp")
	mustWrite(t, replacement, `rules:
  - id: no-todo
    type: forbidden-pattern
    scope: {include: ['**']}
    params: {pattern: BROKEN}
`)
	if err := os.Rename(replacement, filepath.Join(root, ".formwork", "rules", "rules.yaml")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		doc, _ = callCheckFile(t, session, checkFileTestArgs{Path: "a.txt", Content: &content})
		if doc.Status == "findings" && doc.FindingsTotal == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("resident server retained stale rules: %+v", doc)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestServeMCPCheckFileRefusesFailuresWithoutReadingOutsideRoot(t *testing.T) {
	root := contentRepo(t, "", `rules:
  - id: no-todo
    type: forbidden-pattern
    scope: {include: ['**']}
    params: {pattern: TODO}
`)
	session := connectFormworkMCP(t, root)
	_, result := callCheckFile(t, session, checkFileTestArgs{Path: "../outside.txt"})
	if !result.IsError {
		t.Fatalf("outside-root path was accepted: %+v", result)
	}
	_, result = callCheckFile(t, session, checkFileTestArgs{Path: "missing.txt"})
	if !result.IsError {
		t.Fatalf("missing on-disk content was accepted: %+v", result)
	}

	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("TODO outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	doc, result := callCheckFile(t, session, checkFileTestArgs{Path: "link.txt"})
	if result.IsError || doc.Status != "not_scanned" || doc.ContentHash != "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("symlink content was read or wrongly evaluated: doc = %+v; result = %+v", doc, result)
	}
}

func TestServeMCPRejectsInvalidInvocation(t *testing.T) {
	for _, args := range [][]string{{"serve", "-C", t.TempDir()}, {"serve", "--mcp", "extra"}, {"serve", "--mcp", "--bogus"}} {
		var stdout, stderr bytes.Buffer
		code := cli.Run(args, strings.NewReader(""), &stdout, &stderr)
		if code != 2 || stdout.Len() != 0 {
			t.Errorf("args %v: code = %d, stdout = %q, stderr = %q", args, code, stdout.String(), stderr.String())
		}
	}
}
