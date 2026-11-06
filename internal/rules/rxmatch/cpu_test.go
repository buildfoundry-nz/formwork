package rxmatch

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/cputime"
)

func TestCompileRequiresAMeter(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Compile accepted a nil meter")
		}
	}()
	_, _ = Compile("test", "a", "re2", nil)
}

func TestRegexp2ChargesTheCallingRule(t *testing.T) {
	var meter cputime.Meter
	compiled, err := Compile("test", "(a+)+$", "regexp2", &meter)
	if err != nil {
		t.Fatal(err)
	}
	matcher := compiled.(pcreMatcher)
	var input string
	var direct time.Duration
	calibrated := false
	for n := 8; n <= 26; n++ {
		input = strings.Repeat("a", n) + "!"
		runtime.LockOSThread()
		before, ok := cputime.Thread()
		_, matchErr := matcher.re.MatchString(input)
		after, _ := cputime.Thread()
		runtime.UnlockOSThread()
		if !ok {
			t.Skip("thread CPU clock is unsupported")
		}
		if matchErr != nil {
			t.Fatalf("calibration match at length %d failed: %v", n, matchErr)
		}
		direct = after - before
		if direct >= 100*time.Millisecond {
			calibrated = true
			break
		}
	}
	if !calibrated {
		t.Fatal("could not calibrate a regexp2 match to at least 100ms")
	}
	if direct > 700*time.Millisecond {
		t.Skipf("calibration jumped to %s, too near the match timeout for a stable comparison", direct)
	}
	if _, err := matcher.MatchString(input); err != nil {
		t.Fatal(err)
	}
	got := meter.OffThreadCPU()
	if got < direct*70/100 || got > direct*130/100 {
		t.Fatalf("metered regexp2 CPU = %s, direct locked-thread CPU = %s; want within 30%%", got, direct)
	}
}
