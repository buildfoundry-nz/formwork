package rxmatch

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dlclark/regexp2"
)

// Moved here with the backend it tests (#17979): the outer bound now lives in
// rxmatch, which pattern and pair-consistency both compile through, so the test
// follows its subject rather than being left behind on a package that no longer
// declares it.
//
// TestRegexp2OuterBoundFailsClosed: when regexp2's MatchTimeout is disabled
// (the hang under load: its userspace clock is never observed), the Go-runtime
// outer bound must still return an error instead of running for minutes.
func TestRegexp2OuterBoundFailsClosed(t *testing.T) {
	origTimeout, origBound := regexp2MatchTimeout, regexp2OuterBound
	origPatient, origPatientBound := regexp2PatientMatchTimeout, regexp2PatientOuterBound
	regexp2MatchTimeout = time.Duration(math.MaxInt64)
	regexp2OuterBound = 200 * time.Millisecond
	regexp2PatientMatchTimeout = time.Duration(math.MaxInt64)
	regexp2PatientOuterBound = 200 * time.Millisecond
	t.Cleanup(func() {
		regexp2MatchTimeout, regexp2OuterBound = origTimeout, origBound
		regexp2PatientMatchTimeout, regexp2PatientOuterBound = origPatient, origPatientBound
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

// A wall clock running out says the machine was busy or the pattern is slow, and
// only a second attempt tells them apart: a false timeout on a match of a few
// milliseconds took a whole TakeoffQS run down, with a different rule each time.
func TestATimedOutMatchGetsOnePatientAttempt(t *testing.T) {
	for name, first := range map[string]error{
		"regexp2's own timeout": errors.New("match timeout after 1s on input `package a`"),
		"the outer bound":       fmt.Errorf("match exceeded 2s %s", regexp2HardBoundMessage),
	} {
		t.Run(name, func(t *testing.T) {
			m := compilePcre(t, "a")
			var timeouts []time.Duration
			got, err := regexp2Call(m, func(re *regexp2.Regexp) (string, error) {
				timeouts = append(timeouts, re.MatchTimeout)
				if len(timeouts) == 1 {
					return "", first
				}
				return "answer", nil
			})
			if err != nil || got != "answer" {
				t.Fatalf("got %q, %v, want the second attempt's answer", got, err)
			}
			if want := []time.Duration{regexp2MatchTimeout, regexp2PatientMatchTimeout}; !slices.Equal(timeouts, want) {
				t.Fatalf("attempts ran under %v, want %v", timeouts, want)
			}
		})
	}
}

func TestAnErrorThatIsNotATimeoutIsNotRetried(t *testing.T) {
	m := compilePcre(t, "a")
	attempts := 0
	want := errors.New("regexp2 panicked")
	_, err := regexp2Call(m, func(*regexp2.Regexp) (bool, error) {
		attempts++
		return false, want
	})
	if !errors.Is(err, want) || attempts != 1 {
		t.Fatalf("got %v after %d attempt(s), want %v after 1", err, attempts, want)
	}
}

// A real runaway still fails closed once the patient attempt runs out, and an
// error is the only way out: it must not read as a clean pass.
func TestARunawayPatternStillFailsClosedAfterThePatientAttempt(t *testing.T) {
	origTimeout, origPatient := regexp2MatchTimeout, regexp2PatientMatchTimeout
	regexp2MatchTimeout, regexp2PatientMatchTimeout = 50*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { regexp2MatchTimeout, regexp2PatientMatchTimeout = origTimeout, origPatient })

	m := compilePcre(t, "(a+)+$")
	bomb := strings.Repeat("a", 50) + "!"
	ok, err := regexp2Call(m, func(re *regexp2.Regexp) (bool, error) { return re.MatchString(bomb) })
	if err == nil || ok || !regexp2TimedOut(err) {
		t.Fatalf("got %v, %v, want a timeout error", ok, err)
	}
	if _, err := m.MatchString(bomb); err == nil {
		t.Fatal("MatchString must surface the timeout, got nil")
	}
}

// CountMatches can time out mid-walk, so a Match found by the first attempt's
// Regexp reaches the patient copy. The walk must carry on from where it was.
func TestThePatientCopyContinuesAWalkTheFirstAttemptStarted(t *testing.T) {
	m := compilePcre(t, "a")
	first, err := m.re.FindStringMatch("a a a")
	if err != nil || first == nil {
		t.Fatalf("first match: %v, %v", first, err)
	}
	patient, err := regexp2Patient(m.src)
	if err != nil {
		t.Fatal(err)
	}
	second, err := patient.FindNextMatch(first)
	if err != nil || second == nil || second.Index != 2 {
		t.Fatalf("got %v, %v, want the match at index 2", second, err)
	}
}

func compilePcre(t *testing.T, pattern string) pcreMatcher {
	t.Helper()
	c, err := Compile("test", pattern, "regexp2")
	if err != nil {
		t.Fatal(err)
	}
	return c.(pcreMatcher)
}
