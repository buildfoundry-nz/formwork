package config_test

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/stdlib"
)

func expectedCorpusHash(t *testing.T, root string, library string) string {
	t.Helper()
	h := sha256.New()
	write := func(source string, data []byte) {
		fmt.Fprintf(h, "formwork-corpus-v1\x00%s\x00%d\x00", source, len(data))
		h.Write(data)
		h.Write([]byte{0})
	}
	envelope, err := os.ReadFile(filepath.Join(root, ".formwork", "formwork.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	write(".formwork/formwork.yaml", envelope)
	if library != "" {
		pack, err := stdlib.Open(library)
		if err != nil {
			t.Fatal(err)
		}
		names, err := fs.Glob(pack, "*.yaml")
		if err != nil {
			t.Fatal(err)
		}
		sort.Strings(names)
		for _, name := range names {
			data, err := fs.ReadFile(pack, name)
			if err != nil {
				t.Fatal(err)
			}
			write("library:"+library+"/"+name, data)
		}
	}
	names, err := filepath.Glob(filepath.Join(root, ".formwork", "rules", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		write(".formwork/rules/"+filepath.Base(name), data)
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil))
}

func TestCorpusHashUsesExactLoadedBytes(t *testing.T) {
	const envelope = "version: 1\nlibrary: [generic]\ntests: ['**/*_test.go', '**/*_test.dart']\n"
	const local = "rules:\n  - id: local-only\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: TODO}\n"
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":    envelope,
		".formwork/rules/local.yaml": local,
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	want := expectedCorpusHash(t, root, "generic")
	if cfg.CorpusHash != want {
		t.Fatalf("CorpusHash = %q, want hash of exact envelope/library/local bytes %q", cfg.CorpusHash, want)
	}
	again, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if again.CorpusHash != cfg.CorpusHash {
		t.Fatalf("same bytes hashed differently: %q then %q", cfg.CorpusHash, again.CorpusHash)
	}
	if err := os.WriteFile(filepath.Join(root, ".formwork", "rules", "local.yaml"), []byte(local+"# one loaded byte changes the corpus\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if changed.CorpusHash == cfg.CorpusHash {
		t.Fatal("local rule-byte edit did not change corpus hash")
	}
	if changed.CorpusHash != expectedCorpusHash(t, root, "generic") {
		t.Fatal("edited corpus hash does not match the bytes actually loaded")
	}
}

func TestCorpusHashChangesWithEnvelopeBytes(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  "rules: []\n",
	})
	before, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".formwork", "formwork.yaml"), []byte("version: 1\n# exact bytes matter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if before.CorpusHash == after.CorpusHash {
		t.Fatal("envelope-only edit did not change corpus hash")
	}
}
