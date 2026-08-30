package preprocess

import (
	"strings"
	"testing"
)

// TestStringsOnlySh pins the projection that reaches the text shell carries as
// DATA: the awk programs passed as single-quoted arguments, the awk libraries
// carried in heredocs, and the single-quoted `bash -c '…'` worker bodies.
// Those are programs with their own '#' comments, but to the shell lexer they
// are opaque strings, so DestringSh blanks them and every rule reading that
// projection is blind to them.
func TestStringsOnlySh(t *testing.T) {
	cases := map[string]struct {
		src        string
		keep, gone []string
	}{
		"single-quoted awk program is kept": {
			src: `awk '
  # AWKCOMMENTSENTINEL
  { print $1 }
' /dev/null
# SHELLCOMMENTSENTINEL
`,
			keep: []string{"AWKCOMMENTSENTINEL", "{ print $1 }"},
			gone: []string{"SHELLCOMMENTSENTINEL", "awk", "/dev/null"},
		},
		"heredoc body is kept": {
			src: `cat <<'AWKLIB'
# HEREDOCCOMMENTSENTINEL
function trim(s) { return s }
AWKLIB
# SHELLCOMMENTSENTINEL
`,
			keep: []string{"HEREDOCCOMMENTSENTINEL", "function trim"},
			gone: []string{"SHELLCOMMENTSENTINEL", "AWKLIB"},
		},
		"double-quoted string is kept": {
			src: `msg="DOUBLEQUOTEDSENTINEL"
# SHELLCOMMENTSENTINEL
`,
			keep: []string{"DOUBLEQUOTEDSENTINEL"},
			gone: []string{"SHELLCOMMENTSENTINEL", "msg="},
		},
		"ansi-c string is kept": {
			src: `sep=$'ANSICSENTINEL'
# SHELLCOMMENTSENTINEL
`,
			keep: []string{"ANSICSENTINEL"},
			gone: []string{"SHELLCOMMENTSENTINEL"},
		},
		// A heredoc whose terminator never arrives is not a heredoc, so it has
		// no body and this projection keeps nothing from it — the exact
		// complement of DestringSh, which leaves those lines readable as code.
		// Asserting on the pair rather than one side is the point: whichever
		// way the lexer reads the construct, the two projections partition the
		// file, and this case pins which way it reads it.
		"an unterminated heredoc yields no body, matching its sibling": {
			src: `cat <<NOPE
UNTERMINATEDSENTINEL
`,
			gone: []string{"UNTERMINATEDSENTINEL", "cat <<NOPE"},
		},
		"code with no strings projects to nothing": {
			src: `set -euo pipefail
# SHELLCOMMENTSENTINEL
echo done
`,
			gone: []string{"SHELLCOMMENTSENTINEL", "set -euo", "echo done"},
		},
		"empty input": {src: ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			out := string(StringsOnlySh([]byte(c.src)))
			if got, want := strings.Count(out, "\n"), strings.Count(c.src, "\n"); got != want {
				t.Fatalf("line structure changed: %d newlines, want %d", got, want)
			}
			if len(out) != len(c.src) {
				t.Fatalf("length changed: %d bytes, want %d", len(out), len(c.src))
			}
			for _, s := range c.keep {
				if !strings.Contains(out, s) {
					t.Errorf("projection lost %q, which is embedded program text\n--- got ---\n%s", s, out)
				}
			}
			for _, s := range c.gone {
				if strings.Contains(out, s) {
					t.Errorf("projection kept %q, which is shell text\n--- got ---\n%s", s, out)
				}
			}
		})
	}
}

// TestStringsOnlyShIsTheComplementOfDestringSh pins the relationship the two
// projections claim: every byte of the input is blanked by exactly one of
// them, so nothing falls between the pair and nothing is read twice. A rule
// set that splits a ban across these projections is only complete if the
// complement holds — and it holds by construction, because both are written
// over the one scanShData span list.
func TestStringsOnlyShIsTheComplementOfDestringSh(t *testing.T) {
	src := []byte(`#!/usr/bin/env bash
set -euo pipefail
# a shell comment carrying prose
PAT='# an awk comment inside a quoted program'
awk '
  # another one
  { print $1 }
' /dev/null
cat <<'TPL'
# a comment in a heredoc body
TPL
msg="a double-quoted message"
sep=$'\n'
(( mask = 1 << n ))
let other=1<<n
printf '%s %s %s\n' "${PAT}" "${msg}" "${sep}"
`)
	de := DestringSh(src)
	so := StringsOnlySh(src)
	for i := range src {
		// A newline is never blanked by either projection, and a space in the
		// source is indistinguishable from a blanked byte, so those two are
		// the characters the comparison cannot classify.
		if src[i] == '\n' || src[i] == ' ' {
			continue
		}
		deKept := de[i] == src[i]
		soKept := so[i] == src[i]
		if deKept == soKept {
			t.Fatalf("byte %d (%q) kept by both=%v: the projections are not complements\n--- destring ---\n%s\n--- strings-only ---\n%s",
				i, src[i], deKept, de, so)
		}
	}
}

func TestStringsOnlyShRegistered(t *testing.T) {
	if _, ok := Lookup("strings-only-sh"); !ok {
		t.Fatal("strings-only-sh is not registered")
	}
}
