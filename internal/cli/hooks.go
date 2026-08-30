package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/hooks"
)

// The `formwork hooks` command surface. Split out of cli.go when that file came
// within a few lines of this repo's own 750-line hard cap (file-size-vendor-cap;
// it was at 746 and never crossed) — the cure on that rule is "split the file",
// never widen the cap, because consumers that vendor this source enforce it
// downstream. cli_hooks_test.go was split off the
// same way and for the same reason.

// runHooks installs or verifies the git hook shims for git-hook-named lanes
// (spec §8). `verify` replaces the shell system's hooks-wired gate.
func runHooks(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: formwork hooks <install|verify> [flags]")
		return 2
	}
	sub := args[0]
	fs, root := newFlagSet("hooks "+sub, "repository root", stderr)
	// Registered for `install` alone: `verify` changes nothing, so there is
	// nothing for an override to authorise, and defining the flag there would
	// advertise a power the command does not have. Registering per subcommand is
	// also what makes `hooks verify --override-global` a flag-parse error (exit 2)
	// rather than an accepted no-op — the FlagSet is built from sub, and an
	// undefined flag is what flag.ContinueOnError reports to parseAndLoad.
	var overrideGlobal bool
	if sub == "install" {
		fs.BoolVar(&overrideGlobal, "override-global", false,
			"install even though git runs hooks from a setting wider than this repository; writes core.hooksPath in this repository only")
	}
	cfg, ok := parseAndLoad(fs, args[1:], root, stderr)
	if !ok {
		return 2
	}
	switch sub {
	case "install":
		installed, err := hooks.Install(*root, cfg, overrideGlobal)
		// Install reports what it wired AND what it refused: a stale lane must
		// not cost the operator the hooks that are healthy, but it must still be
		// loud. Print both, in that order, then exit 2 on the refusal.
		if len(installed) > 0 {
			fmt.Fprintln(stdout, "formwork: installed git hooks: "+strings.Join(installed, ", "))
		}
		if err != nil {
			fmt.Fprintln(stderr, "formwork:", err)
			return 2
		}
		return 0
	case "verify":
		// A git failure is exit 2, not exit 1. Verify's problems describe the
		// repository's wiring; its error means verify could not find out what
		// git will do, and reporting "not wired" for a question nobody answered
		// would be a layout diagnosis invented from a tool failure.
		problems, err := hooks.Verify(*root, cfg)
		if err != nil {
			fmt.Fprintln(stderr, "formwork:", err)
			return 2
		}
		if len(problems) > 0 {
			for _, p := range problems {
				fmt.Fprintln(stdout, "formwork: hooks: "+p)
			}
			return 1
		}
		fmt.Fprintln(stdout, "formwork: hooks wired")
		return 0
	default:
		fmt.Fprintf(stderr, "formwork: unknown hooks subcommand %q\n", sub)
		return 2
	}
}
