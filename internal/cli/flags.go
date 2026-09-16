package cli

import (
	"flag"
	"fmt"
	"io"
)

// Flag-value guards shared by the commands that declare the same flags. Each
// refuses, at the CLI seam that knows the flag was supplied, a value the run
// cannot honour — a supplied flag silently becoming a different run is the
// shape both guards exist to close (#154 for --range, #156 for --workers).
// Split out of cli.go to keep every part under the vendored 750-line cap.

// A shared helper is what stops a third caller diverging again.
func rangeValueUsable(fs *flag.FlagSet, rangeSpec, fallback string, stderr io.Writer) bool {
	given := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "range" {
			given = true
		}
	})
	if given && rangeSpec == "" {
		fmt.Fprintf(stderr, "formwork: --range was given an empty value — refusing to fall back to %s; pass a real range (e.g. origin/main..HEAD) or drop the flag\n", fallback)
		return false
	}
	return true
}

// workersValueUsable reports whether --workers is safe to act on, printing the
// refusal itself when it is not. Same shape as rangeValueUsable above and for
// the same reason: a supplied flag whose value the run cannot honour must not
// quietly become a different run.
//
// engine.Run reads `workers <= 0` as GOMAXPROCS, which is the right default for
// an ABSENT flag and is left exactly as it is. What that seam cannot see is
// whether the number it was handed is a default or a value the CLI declined to
// honour — a width is all it receives — so the distinction is drawn here, at the
// seam that knows the flag exists and can name it in the refusal. Both commands
// that declare the flag call this before doing anything with the value; `test`
// spends it one hop further away (fixturetest.Run forwards it to engine.Run,
// run.go:155), which is exactly why the guard is not left to the engine.
//
// No fs.Visit, unlike the --range guard, and the difference is in the values
// rather than in the shape: both declaration sites default this flag to 0 (the
// `fs.Int("workers", 0, ...)` calls in runCheck and runTest), and 0 is also a
// legal supplied value meaning the same thing — so absent and supplied-0 are
// genuinely indistinguishable AND genuinely identical, while a NEGATIVE value
// can only have been typed. Refusing 0 would exit 2 on every ordinary
// invocation, which is a worse bug than the one this guard closes.
func workersValueUsable(workers int, stderr io.Writer) bool {
	if workers < 0 {
		fmt.Fprintf(stderr, "formwork: --workers %d is not a worker count — refusing to fall back to GOMAXPROCS, which is the opposite of the throttle you asked for; pass a positive count, or drop the flag\n", workers)
		return false
	}
	return true
}
