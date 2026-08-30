package command

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSnippetEmpty(t *testing.T) {
	if got := snippet(nil); got != "" {
		t.Fatalf("nil: got %q", got)
	}
	if got := snippet([]byte("  \n\t")); got != "" {
		t.Fatalf("whitespace-only: got %q", got)
	}
}

func TestSnippetShortUntruncated(t *testing.T) {
	got := snippet([]byte("hello"))
	if got != ": hello" {
		t.Fatalf("got %q, want %q", got, ": hello")
	}
}

// TestSnippetKeepsHeadAndTail proves that when output exceeds the budget both
// ends survive: per-finding lines (head) and the actionable summary (tail).
// Pure-head truncation was the original bug; pure-tail truncation dropped
// finding names that lockdown synth tests assert on.
func TestSnippetKeepsHeadAndTail(t *testing.T) {
	const headMarker = "HEAD-FINDING-NAME"
	const tailMarker = "TAIL-CURE-SUMMARY"
	// Pad the middle so total length well exceeds max=800.
	out := headMarker + strings.Repeat("M", 1000) + tailMarker
	got := snippet([]byte(out))
	if !strings.Contains(got, headMarker) {
		t.Fatalf("expected head marker in snippet, got %q", got)
	}
	if !strings.Contains(got, tailMarker) {
		t.Fatalf("expected tail marker in snippet, got %q", got)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("truncated snippet should contain ellipsis, got %q", got)
	}
	if !strings.HasPrefix(got, ": "+headMarker) {
		t.Fatalf("snippet should open with head marker, got %q", got[:min(40, len(got))])
	}
}

// TestSnippetRuneBoundary covers multi-byte runes straddling both the head
// cut and the tail cut: a naive byte slice would emit a leading or trailing
// continuation byte and fail UTF-8 validation. Both cuts must land on a rune
// start/end.
func TestSnippetRuneBoundary(t *testing.T) {
	const dash = "—" // U+2014, three UTF-8 bytes e2 80 94

	// headBudget=400, tailBudget=800-400-len("…")=397.
	// Place a dash so naive head cut (byte 400) lands mid-dash, and naive
	// tail start lands mid-dash too.
	prefix := strings.Repeat("P", 399) // dash starts at 399; byte 400 is mid-dash
	// len = 399+3+midPad+3+suffixLen
	// naiveTail = len - 397 should be mid tail-dash (second byte of dash)
	// tailDashStart = 399+3+midPad = 402+midPad
	// want 402+midPad + 1 == len - 397
	// len = 402+midPad + 1 + 397 = midPad + 800
	// also len = 399+3+midPad+3+suffixLen = midPad + 405 + suffixLen
	// => suffixLen = 395
	suffix := strings.Repeat("S", 392) + "END" // 395
	midPad := strings.Repeat("M", 300)
	s := prefix + dash + midPad + dash + suffix

	if utf8.RuneStart(s[400]) {
		t.Fatal("test setup: naive head cut must be mid-rune")
	}
	naiveTail := len(s) - 397
	if utf8.RuneStart(s[naiveTail]) {
		t.Fatalf("test setup: naive tail start %d must be mid-rune (len=%d)", naiveTail, len(s))
	}
	if len(s) <= 800 {
		t.Fatalf("test setup: input must exceed max, got len=%d", len(s))
	}

	got := snippet([]byte(s))
	if !utf8.ValidString(got) {
		t.Fatalf("snippet produced invalid UTF-8: %q (bytes %v)", got, []byte(got))
	}
	if !strings.Contains(got, "END") {
		t.Fatalf("expected tail END in %q", got)
	}
	if !strings.Contains(got, "PPP") {
		t.Fatalf("expected head P's in %q", got)
	}
	body := strings.TrimPrefix(got, ": ")
	if !utf8.ValidString(body) {
		t.Fatalf("snippet body invalid UTF-8: %q", body)
	}
	for _, part := range strings.Split(body, "…") {
		if len(part) > 0 && !utf8.RuneStart(part[0]) {
			t.Fatalf("segment starts mid-rune: first byte 0x%02x in %q", part[0], part[:min(20, len(part))])
		}
		if !utf8.ValidString(part) {
			t.Fatalf("segment invalid UTF-8: %q", part)
		}
	}
}

// TestSnippetExactlyMaxLeavesWholeString ensures a string of exactly max bytes
// is not truncated (no ellipsis).
func TestSnippetExactlyMaxLeavesWholeString(t *testing.T) {
	s := strings.Repeat("x", 800)
	got := snippet([]byte(s))
	if got != ": "+s {
		t.Fatalf("exactly-max should be untruncated, got len=%d prefix=%q", len(got), got[:min(10, len(got))])
	}
}

// TestSnippetPreservesLongHeaderFindingName covers the asadmin shape: a
// long detector header, then "  - path:func#N" on the next line within the
// first 400 bytes. Head budget must keep that name.
func TestSnippetPreservesLongHeaderFindingName(t *testing.T) {
	header := "[check-asadmin-bypass-sites-allowlist] FAIL — 1 RLS-bypass call site(s) (db.AsSuperuser or raw SET [LOCAL] ROLE to a BYPASSRLS role) with no anchor in .formwork/allowlists/asadmin-bypass-sites.txt:\n  - jobs.go:flip#1\n"
	cure := strings.Repeat("CURE ", 200) + "FINAL-CURE"
	out := header + strings.Repeat("x", 500) + cure
	got := snippet([]byte(out))
	if !strings.Contains(got, "jobs.go:flip#1") {
		t.Fatalf("expected finding name in snippet, got %q", got)
	}
	if !strings.Contains(got, "FINAL-CURE") {
		t.Fatalf("expected cure tail in snippet, got %q", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
