package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/Masterminds/semver/v3"
)

// checkEngine gates a run against a formwork.yaml `engine:` constraint. It is
// pure (a seam for tests): the caller supplies both the binary's raw
// self-report and its trusted (gate) version, plus whether the dev-build
// opt-out is set. It returns (warn, err):
//   - warn non-empty: emit to stderr and proceed;
//   - err non-nil: refuse to run (the caller maps this to exit 2).
//
// Both a raw and a trusted version are required, not just the trusted one,
// because trustedVersion collapses an UNTRUSTED string (a dirty, describe, or
// pseudo-version stamp) to the "dev" sentinel while keeping every genuine
// release stamp — INCLUDING a prerelease release such as an RC or a default
// GoReleaser --snapshot build (0.2.1-SNAPSHOT-<sha>), which isReleaseVersion
// accepts. Such a prerelease is therefore checked on the trusted path below
// and refused there by Masterminds' plain-constraint prerelease exclusion
// (>=0.2.0 admits neither 1.0.0-rc.1 nor a --snapshot stamp; that is
// deliberate and fail-closed — an author who wants prereleases in must write
// >=0.2.0-0). It never reaches the dev fallback.
//
// FORMWORK_ALLOW_DEV exists to cover binaries whose version
// cannot be identified at all; it must never be able to wave through a
// binary we CAN identify as too old just because that identification wasn't
// trustworthy enough to satisfy the constraint on its own. So: a trusted
// version is checked normally; a trusted "dev" falls back to the raw
// version — if the raw version parses and demonstrably fails the
// constraint, that is refused unconditionally (even with allowDev set);
// only when the raw version can't even be evaluated does allowDev's warnable
// fail-open apply.
func checkEngine(raw string, constraint *semver.Constraints, rawVersion, trustedVersion string, allowDev bool) (warn string, err error) {
	if constraint == nil {
		return "", nil // no engine: field — nothing to enforce
	}
	if trustedVersion != "dev" {
		v, verr := semver.NewVersion(trustedVersion)
		if verr != nil {
			return "", fmt.Errorf("version %q is not valid semver, cannot check engine constraint %q: %w", trustedVersion, raw, verr)
		}
		if !constraint.Check(v) {
			return "", fmt.Errorf("%s does not satisfy engine constraint %q declared in formwork.yaml", trustedVersion, raw)
		}
		return "", nil
	}
	// trustedVersion == "dev": the binary's identity could not be verified as a
	// release. But hard evidence beats the opt-out — if the raw self-report
	// parses as semver and demonstrably fails the constraint, refuse
	// regardless of allowDev; the opt-out is for binaries we cannot identify,
	// not for discarding a version we CAN identify as too old.
	if rawVersion != "dev" {
		if v, verr := semver.NewVersion(rawVersion); verr == nil && !constraint.Check(v) {
			return "", fmt.Errorf("%s does not satisfy engine constraint %q declared in formwork.yaml", rawVersion, raw)
		}
	}
	if allowDev {
		if rawVersion != "dev" {
			return fmt.Sprintf("dev build (reported version %q, untrusted for gating) does not enforce engine constraint %q (FORMWORK_ALLOW_DEV set)", rawVersion, raw), nil
		}
		return fmt.Sprintf("dev build does not enforce engine constraint %q (FORMWORK_ALLOW_DEV set)", raw), nil
	}
	if rawVersion != "dev" {
		return "", fmt.Errorf("dev build (reported version %q, untrusted for gating) cannot verify engine constraint %q declared in formwork.yaml; install a released binary, or set FORMWORK_ALLOW_DEV=1 to override for local use", rawVersion, raw)
	}
	return "", fmt.Errorf("dev build cannot verify engine constraint %q declared in formwork.yaml; install a released binary, or set FORMWORK_ALLOW_DEV=1 to override for local use", raw)
}

// enforceEngine wires checkEngine to the real binary version and environment,
// printing any warning or error to stderr. It returns false if the run must
// abort (the caller then returns exit code 2). raw and constraint come from
// the formwork.yaml envelope (config.Envelope) — this runs before any rule
// file is parsed (spec §4).
func enforceEngine(raw string, constraint *semver.Constraints, stderr io.Writer) bool {
	// No engine: field — nothing to enforce, and the whole backstop
	// (including its opt-out) is inert. Checking this before touching
	// FORMWORK_ALLOW_DEV at all keeps "absent engine: is completely inert"
	// true: a repo with no constraint must never print anything here, no
	// matter what junk is sitting in that env var (finding 5).
	if constraint == nil {
		return true
	}
	// ParseBool so an explicit FORMWORK_ALLOW_DEV=0/false does NOT enable the
	// opt-out; anything unparseable (or empty) stays fail-closed (allowDev=false).
	allowDevRaw := os.Getenv("FORMWORK_ALLOW_DEV")
	allowDev, _ := strconv.ParseBool(allowDevRaw)
	bv, ok := buildInfoVersion()
	rv := rawVersion(version, bv, ok)
	tv := gateVersionFrom(version, bv, ok)
	// Warn about an unparseable value only where the opt-out could actually
	// apply (the dev-build path). On a trusted release allowDev is never
	// consulted, so the notice would be spurious stderr noise (#16).
	if w := devOptOutWarning(tv, allowDevRaw); w != "" {
		fmt.Fprintln(stderr, "formwork:", w)
	}
	warn, err := checkEngine(raw, constraint, rv, tv, allowDev)
	if warn != "" {
		fmt.Fprintln(stderr, "formwork:", warn)
	}
	if err != nil {
		fmt.Fprintln(stderr, "formwork:", err)
		return false
	}
	return true
}

// devOptOutWarning returns the notice to print when FORMWORK_ALLOW_DEV holds an
// unparseable value, or "" when nothing should be printed. It fires only on the
// dev-build path (trusted gate version "dev"), where the opt-out could actually
// change the outcome. On a trusted release the opt-out is never consulted, so an
// unparseable value is irrelevant and must stay silent (#16) — mirroring the
// "absent engine: field prints nothing" guarantee one level up. The returned
// string carries no "formwork:" prefix; the caller adds it via Fprintln.
func devOptOutWarning(trustedVersion, allowDevRaw string) string {
	if trustedVersion != "dev" || allowDevRaw == "" {
		return ""
	}
	if _, err := strconv.ParseBool(allowDevRaw); err == nil {
		return ""
	}
	return fmt.Sprintf("ignoring unparseable FORMWORK_ALLOW_DEV=%q (use 1 or 0)", allowDevRaw)
}

// devOptOutActive reports whether FORMWORK_ALLOW_DEV would actually let THIS
// binary skip a declared engine constraint: the env var must parse truthy
// AND the binary's trusted gate version must be the "dev" sentinel. A
// genuine release binary's gate version is never "dev" (trustedVersion
// collapses only unreleased/unidentifiable builds to it), so the opt-out is
// inert for it regardless of the env var — `formwork lint`'s escape-hatch
// enumeration must not claim the backstop is DISABLED on a binary where it
// is, in fact, fully enforcing (finding 3). meta.Lint lives in a package
// that cannot import cli (cli already imports meta), so this fact is
// computed here and passed in rather than recomputed with an inverted
// dependency.
func devOptOutActive() bool {
	allowDev, err := strconv.ParseBool(os.Getenv("FORMWORK_ALLOW_DEV"))
	return err == nil && allowDev && gateVersion() == "dev"
}
