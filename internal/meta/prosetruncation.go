package meta

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// proseTruncated reports plain (unquoted) YAML scalars whose text runs past a
// ` #`, because YAML ends the value there and treats the rest as a comment.
// The engine receives the head and reports it — as a cure, a message, an
// origin — with the tail gone and nothing said about it (#59):
//
//	cure: converted call sites consume the accessor (audit-1 #14)
//
// arrives as "converted call sites consume the accessor (audit-1". Issue-number
// references in cures are the house style, and exactly the text YAML eats.
//
// WHY THIS REPORTS INSTEAD OF DECIDING
// `cure: do the thing  # note to self` is legitimate YAML with a deliberate
// trailing comment, and by the time the decoder hands over a string the two
// cases are identical — the comment is gone either way. No amount of care
// distinguishes them, so this asks the author to QUOTE, which makes the intent
// explicit whichever it was. Quoting clears the report; that is the cure.
//
// Detected from the raw bytes rather than the decoded config, because the
// decoded value no longer carries the evidence.
//
// TWO NARROWINGS, both load-bearing:
//
//  1. `#` is only a comment when whitespace precedes it. `see issue#14` keeps
//     its hash, so a glued one is never reported.
//  2. The retained head must itself contain whitespace, i.e. read as prose. A
//     single-token value followed by a comment (`cap: 40  # taste`) is the
//     ordinary, intentional shape and reporting it would make the check noise.
//     This is the narrowing that keeps it quiet on real corpora.
var plainScalarWithComment = regexp.MustCompile(`^[ \t]*([A-Za-z_][A-Za-z0-9_-]*):[ \t]+([^'"|>&*!\[{#\s][^#]*?)[ \t]+#`)

func proseTruncated(root string) ([]string, error) {
	rulesDir := filepath.Join(root, ".formwork", "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("prose-not-truncated: reading %s: %w", rulesDir, err)
	}

	var problems []string
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names) // deterministic output regardless of directory order

	for _, name := range names {
		path := filepath.Join(rulesDir, name)
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("prose-not-truncated: reading %s: %w", path, err)
		}
		sc := bufio.NewScanner(f)
		for line := 1; sc.Scan(); line++ {
			m := plainScalarWithComment.FindStringSubmatch(sc.Text())
			if m == nil {
				continue
			}
			key, kept := m[1], strings.TrimRight(m[2], " \t")
			if !strings.ContainsAny(kept, " \t") {
				continue // narrowing 2: a single token plus a comment
			}
			problems = append(problems, fmt.Sprintf(
				"%s:%d: %s is an unquoted scalar and YAML ended it at the ' #' — the engine receives %q and the rest is discarded. Quote the value if the text continues; quote it anyway if the ' #' really began a comment, so the next reader can tell.",
				filepath.Join(".formwork", "rules", name), line, key, kept))
		}
		closeErr := f.Close()
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("prose-not-truncated: reading %s: %w", path, err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("prose-not-truncated: closing %s: %w", path, closeErr)
		}
	}
	return problems, nil
}
