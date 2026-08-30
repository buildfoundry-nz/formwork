package scan_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/preprocess"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// This registration lives only in scan's test binary; it cannot leak into
// internal/preprocess's own Names() test, which runs in a separate,
// per-package test binary.
func init() {
	preprocess.Register("test-broken-drops-lines", func(content []byte) []byte {
		return bytes.ReplaceAll(content, []byte("\n"), []byte(" "))
	})
}

func walkOne(t *testing.T, name, content string) *scan.File {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, name), content)
	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(fset.Files) != 1 {
		t.Fatalf("files: %d", len(fset.Files))
	}
	return fset.Files[0]
}

func TestVariantRawReturnsSelf(t *testing.T) {
	f := walkOne(t, "a.go", "code() // c\n")
	for _, name := range []string{"", "raw"} {
		v, err := f.Variant(name)
		if err != nil {
			t.Fatal(err)
		}
		if v != f {
			t.Fatalf("Variant(%q) returned a new file, want the original", name)
		}
	}
}

func TestVariantTransformsAndCaches(t *testing.T) {
	f := walkOne(t, "a.go", "code() // secret\n")
	v1, err := f.Variant("decomment-go")
	if err != nil {
		t.Fatal(err)
	}
	content, err := v1.Content()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "secret") {
		t.Fatalf("variant content not transformed: %q", content)
	}
	if v1.Path() != f.Path() {
		t.Fatalf("variant path %q, want %q", v1.Path(), f.Path())
	}
	v2, err := f.Variant("decomment-go")
	if err != nil {
		t.Fatal(err)
	}
	if v1 != v2 {
		t.Fatal("variant not cached: second call returned a different instance")
	}
}

func TestVariantUnknownNameErrors(t *testing.T) {
	f := walkOne(t, "a.go", "x\n")
	if _, err := f.Variant("no-such"); err == nil || !strings.Contains(err.Error(), "no-such") {
		t.Fatalf("unknown variant accepted: %v", err)
	}
}

func TestVariantRejectsLineDroppingTransform(t *testing.T) {
	f := walkOne(t, "a.txt", "line one\nline two\nline three\n")
	_, err := f.Variant("test-broken-drops-lines")
	if err == nil {
		t.Fatal("expected an error for a line-dropping transform, got nil")
	}
	if !strings.Contains(err.Error(), "test-broken-drops-lines") {
		t.Fatalf("error %q does not name the transform", err.Error())
	}
	if !strings.Contains(err.Error(), "a.txt") {
		t.Fatalf("error %q does not name the file", err.Error())
	}
}

func TestVariantConcurrentAccess(t *testing.T) {
	f := walkOne(t, "a.go", "code() // c\nmore()\n")
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := f.Variant("decomment-go")
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := v.Lines(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
