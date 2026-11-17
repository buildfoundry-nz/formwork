package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/fsnotify/fsnotify"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type checkFileInput struct {
	Path    string  `json:"path" jsonschema:"root-relative or absolute-under-root file path to evaluate"`
	Content *string `json:"content,omitempty" jsonschema:"complete unsaved file content; omit to read the on-disk file"`
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func runServe(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs, root := newFlagSet("serve", "repository root whose configuration governs check_file calls", stderr)
	mcpFlag := fs.Bool("mcp", false, "serve the Model Context Protocol over stdin/stdout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 || !*mcpFlag {
		fmt.Fprintln(stderr, "usage: formwork serve --mcp [-C root]")
		return 2
	}
	corpus, err := newCheckFileCorpus(*root)
	if err != nil {
		fmt.Fprintln(stderr, "formwork: watch corpus:", err)
		return 2
	}
	defer corpus.Close()
	server := mcp.NewServer(&mcp.Implementation{Name: "formwork", Version: displayVersion()}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "check_file",
		Description: "Evaluate one complete file against the repository's content-eligible Formwork rules, using supplied unsaved content or the on-disk file.",
	}, corpus.handler())
	if err := server.Run(context.Background(), &mcp.IOTransport{Reader: io.NopCloser(stdin), Writer: nopWriteCloser{stdout}}); err != nil {
		fmt.Fprintln(stderr, "formwork: MCP server:", err)
		return 2
	}
	return 0
}

type checkFileCorpus struct {
	root             string
	watcher          *fsnotify.Watcher
	generation       atomic.Uint64
	reliable         atomic.Bool
	mu               sync.Mutex
	cfg              *config.Config
	loadedGeneration uint64
}

func newCheckFileCorpus(root string) (*checkFileCorpus, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	corpus := &checkFileCorpus{root: root, watcher: watcher}
	corpus.reliable.Store(true)
	for _, dir := range []string{".formwork", filepath.Join(".formwork", "rules")} {
		if err := watcher.Add(filepath.Join(root, dir)); err != nil {
			watcher.Close()
			return nil, err
		}
	}
	go corpus.watch()
	return corpus, nil
}

func (c *checkFileCorpus) Close() {
	c.watcher.Close()
}

func (c *checkFileCorpus) watch() {
	events := c.watcher.Events
	errors := c.watcher.Errors
	for events != nil || errors != nil {
		select {
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
				c.generation.Add(1)
			}
		case _, ok := <-errors:
			if !ok {
				errors = nil
				continue
			}
			c.reliable.Store(false)
		}
	}
	c.reliable.Store(false)
}

func (c *checkFileCorpus) drainEvents() {
	for {
		select {
		case event, ok := <-c.watcher.Events:
			if !ok {
				c.reliable.Store(false)
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
				c.generation.Add(1)
			}
		default:
			return
		}
	}
}

func (c *checkFileCorpus) load() (*config.Config, error) {
	c.drainEvents()
	c.mu.Lock()
	defer c.mu.Unlock()
	generation := c.generation.Load()
	if c.cfg != nil && c.reliable.Load() && c.loadedGeneration == generation {
		return c.cfg, nil
	}
	var diagnostics bytes.Buffer
	cfg, ok := loadGated(c.root, &diagnostics)
	if !ok {
		return nil, errors.New(strings.TrimSpace(diagnostics.String()))
	}
	if diagnostics.Len() > 0 {
		fmt.Fprint(os.Stderr, diagnostics.String())
	}
	c.cfg = cfg
	c.loadedGeneration = generation
	return cfg, nil
}

func (c *checkFileCorpus) handler() mcp.ToolHandlerFor[checkFileInput, any] {
	return func(_ context.Context, _ *mcp.CallToolRequest, input checkFileInput) (*mcp.CallToolResult, any, error) {
		started := time.Now()
		cfg, err := c.load()
		if err != nil {
			return nil, nil, err
		}
		normalized, err := newQueryNormalizer(c.root).normalize(input.Path)
		if err != nil {
			return nil, nil, err
		}
		var content []byte
		if input.Content != nil {
			content = []byte(*input.Content)
		} else {
			nonRegular, _, reachErr := walkReachability(c.root, normalized)
			if reachErr != nil {
				return nil, nil, reachErr
			}
			if !nonRegular {
				content, err = os.ReadFile(filepath.Join(c.root, filepath.FromSlash(normalized)))
				if err != nil {
					return nil, nil, fmt.Errorf("read %s: %w", normalized, err)
				}
			}
		}
		gitIgnore := checkContentResolveGitIgnore(cfg, c.root, []string{normalized})
		doc, _, err := evaluateCheckContent(cfg, gitIgnore, c.root, normalized, content, true, started)
		if err != nil {
			return nil, nil, err
		}
		summary := fmt.Sprintf("%s: %d finding(s)", doc.Status, doc.FindingsTotal)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: summary}}}, doc, nil
	}
}
