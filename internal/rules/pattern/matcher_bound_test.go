package pattern

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/dlclark/regexp2"
)

// TestRegexp2OuterBoundFailsClosed: when regexp2's MatchTimeout is disabled
// (the hang under load: its userspace clock is never observed), the Go-runtime
// outer bound must still return an error instead of running for minutes.
func TestRegexp2OuterBoundFailsClosed(t *testing.T) {
	origTimeout, origBound := regexp2MatchTimeout, regexp2OuterBound
	regexp2MatchTimeout = time.Duration(math.MaxInt64)
	regexp2OuterBound = 200 * time.Millisecond
	t.Cleanup(func() {
		regexp2MatchTimeout, regexp2OuterBound = origTimeout, origBound
	})

	re, err := regexp2.Compile("(a+)+$", regexp2.None)
	if err != nil {
		t.Fatal(err)
	}
	re.MatchTimeout = regexp2MatchTimeout
	m := pcreMatcher{re: re, src: "(a+)+$"}
	bomb := strings.Repeat("a", 50) + "!"

	start := time.Now()
	_, err = m.MatchString(bomb)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("outer bound must fail closed, got nil")
	}
	if !strings.Contains(err.Error(), "hard bound") {
		t.Fatalf("want hard-bound error, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("outer bound took %s, want around %s", elapsed, regexp2OuterBound)
	}
}
