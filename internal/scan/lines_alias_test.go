// lines_alias_test.go — Lines() must not retain a second copy of the file.
//
// The scan cache is the engine's largest steady-state allocation, and Lines()
// was the biggest single term in it: it materialised `string(content)` — a full
// copy — and split that. Since content is already cached and never mutated, the
// copy bought nothing. Measured over a real 10k-file tree, a file cost 1.06x its
// size with Content() alone and 2.55x once Lines() was called, and that 2.55x
// applied again per preprocessor variant (#66).
package scan_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

func heapAllocBytes() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// TestLinesDoesNotRetainASecondCopyOfContent pins the allocation contract, not
// an implementation detail: whatever Lines() does internally, what it RETAINS
// must be the line index, not the file text over again. The budget is half the
// content size — comfortably above the ~16 bytes/line of slice headers the index
// legitimately costs, and comfortably below the full second copy.
func TestLinesDoesNotRetainASecondCopyOfContent(t *testing.T) {
	const (
		line  = "\tif err := doSomething(ctx, arg); err != nil { return err }\n"
		count = 150_000
	)
	content := []byte(strings.Repeat(line, count))

	f := scan.NewMemFile("big.go", content)
	if _, err := f.Content(); err != nil { // ensure content is cached before measuring
		t.Fatal(err)
	}

	before := heapAllocBytes()
	lines, err := f.Lines()
	if err != nil {
		t.Fatal(err)
	}
	after := heapAllocBytes()

	if got, want := len(lines), count; got != want {
		t.Fatalf("Lines() returned %d lines, want %d", got, want)
	}

	retained := after - before
	budget := uint64(len(content) / 2)
	if retained > budget {
		t.Fatalf("Lines() retained %d bytes for a %d-byte file (budget %d): it is holding a second "+
			"copy of the content, which is already cached and never mutated (#66)",
			retained, len(content), budget)
	}
	runtime.KeepAlive(lines)
	runtime.KeepAlive(f)
}

// TestLinesContentUnchangedAcrossVariantAndReread is the safety half. Aliasing
// the cached bytes is only sound while nothing mutates them, so this pins the
// two ways a mutation would surface: building a preprocessor variant from the
// same file, and reading Content() again afterwards. Both must leave previously
// returned lines byte-identical.
func TestLinesContentUnchangedAcrossVariantAndReread(t *testing.T) {
	src := []byte("package p\n// a comment\nfunc f() {}\n")
	f := scan.NewMemFile("a.go", src)

	lines, err := f.Lines()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := append([]string(nil), lines...)

	// A variant transform reads the same content and must not write to it.
	if _, err := f.Variant("decomment-go"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Content(); err != nil {
		t.Fatal(err)
	}

	again, err := f.Lines()
	if err != nil {
		t.Fatal(err)
	}
	for i := range snapshot {
		if again[i] != snapshot[i] {
			t.Fatalf("line %d changed after building a variant: %q -> %q — cached content was mutated, "+
				"which breaks the aliasing invariant Lines() depends on", i, snapshot[i], again[i])
		}
	}
}
