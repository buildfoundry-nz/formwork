package cli

import (
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// version is the release version, overridden at build time via
//
//	-ldflags "-X github.com/buildfoundry-nz/formwork/internal/cli.version=<tag>"
//
// An unstamped build reports "dev".
var version = "dev"

// pseudoVersionSuffix matches Go module pseudo-version suffixes: a 14-digit
// UTC timestamp and a 12-hex-digit commit prefix, optionally preceded by a
// "0." marker and (for a prerelease base) its prerelease tag — covering all
// three pseudo-version forms Go produces:
//
//	vX.0.0-yyyymmddhhmmss-abcdefabcdef        (no known base version)
//	vX.Y.Z-0.yyyymmddhhmmss-abcdefabcdef       (tagged base version)
//	vX.Y.Z-pre.0.yyyymmddhhmmss-abcdefabcdef   (prerelease base version)
var pseudoVersionSuffix = regexp.MustCompile(`-(?:(?:[0-9A-Za-z.-]+\.)?0\.)?\d{14}-[0-9a-f]{12}$`)

// describeSuffix matches `git describe` output for a commit ahead of its tag —
// "-<count>-g<hash>". Such a string names a commit, not a released tag. git
// allows abbreviated hashes down to 4 hex digits (the historical default
// core.abbrev floor), so the hash class must not require 7.
var describeSuffix = regexp.MustCompile(`-\d+-g[0-9a-f]{4,}$`)

// dirtyMarker matches "dirty" as a self-contained component of a version
// string — bounded by "-", "+", ".", or the start/end of the string — rather
// than requiring it to be a trailing suffix. A HasSuffix check on "-dirty"/
// "+dirty" misses a dirty marker followed by further build metadata (e.g.
// "-dirty+build.7", "+dirty.1"); this catches "dirty" wherever it appears as
// its own component.
var dirtyMarker = regexp.MustCompile(`(^|[-+.])dirty([-+.]|$)`)

// buildInfoVersion reports the module version debug.ReadBuildInfo() sees, and
// whether build info was available at all.
func buildInfoVersion() (string, bool) {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version, true
	}
	return "", false
}

// rawVersion resolves what this binary reports about itself: the ldflags stamp
// if present, else the build-info version, else the "dev" sentinel. It makes no
// judgement about whether that version identifies a RELEASED build — that is
// trustedVersion's job. Keeping the two separate is deliberate: `formwork
// version` must not erase a real stamp just because it is untrustworthy for
// gating.
func rawVersion(ldflags, buildVersion string, haveBuild bool) string {
	if ldflags != "" && ldflags != "dev" {
		return ldflags
	}
	if haveBuild && buildVersion != "" && buildVersion != "(devel)" {
		return buildVersion
	}
	return "dev"
}

// trustedVersion returns raw when it identifies a genuine released build, and
// the "dev" sentinel otherwise. This is the value the engine backstop compares
// against a constraint: a version we cannot trust must never satisfy one,
// regardless of how it reached the binary. Applying it here — rather than only
// on the build-info path — is what keeps ldflags and build info symmetric.
func trustedVersion(raw string) string {
	if isReleaseVersion(raw) {
		return raw
	}
	return "dev"
}

// isReleaseVersion reports whether v is a real tagged semver — not the empty
// string, not "dev", not "(devel)", parseable as semver, not a dirty working
// tree, not a `git describe` commit-ahead-of-tag string, and not a
// pseudo-version. Only such versions may be treated as a release.
//
// A leading "v" is NOT required: GoReleaser's `{{ .Version }}` template value
// — what `.goreleaser.yaml` stamps into every released binary — is the tag
// with its "v" stripped (tag v0.2.0 stamps "0.2.0"). Requiring a "v" prefix
// here rejects every officially released binary; semver validity is the real
// test, and Masterminds semver accepts both "0.2.0" and "v0.2.0".
func isReleaseVersion(v string) bool {
	if v == "" || v == "dev" || v == "(devel)" {
		return false
	}
	if _, err := semver.NewVersion(v); err != nil {
		return false
	}
	// A dirty working tree means the binary does not correspond to any released
	// commit. git spells this "-dirty" (git describe --dirty, the default) and
	// semver build metadata spells it "+dirty"; dirtyMarker catches both
	// spellings anywhere in the string, including followed by further metadata.
	if dirtyMarker.MatchString(v) {
		return false
	}
	// Strip semver build metadata before the suffix tests: Go stamps
	// pseudo-versions that may carry a +suffix, and an anchored match would
	// otherwise miss them. `+incompatible` is a legitimate release suffix, so
	// only the metadata is stripped — the version is not rejected.
	base := v
	if i := strings.IndexByte(base, '+'); i >= 0 {
		base = base[:i]
	}
	if describeSuffix.MatchString(base) {
		return false
	}
	return !pseudoVersionSuffix.MatchString(base)
}

// annotate appends an "(unreleased build)" marker to a non-dev raw version
// that is not a genuine release, so `formwork version` shows the distinction
// without destroying the real stamp. Pure helper, split out of displayVersion
// for direct testing.
func annotate(raw string) string {
	if raw == "dev" {
		return "dev"
	}
	if !isReleaseVersion(raw) {
		return raw + " (unreleased build)"
	}
	return raw
}

// displayVersion is what `formwork version` prints: the binary's real
// self-report, annotated when it is not a released build so the distinction is
// visible without being destroyed.
func displayVersion() string {
	bv, ok := buildInfoVersion()
	return annotate(rawVersion(version, bv, ok))
}

// gateVersionFrom is the pure form of gateVersion (seam for tests).
func gateVersionFrom(ldflags, buildVersion string, haveBuild bool) string {
	return trustedVersion(rawVersion(ldflags, buildVersion, haveBuild))
}

// gateVersion is the version the engine backstop compares against a
// constraint.
func gateVersion() string {
	bv, ok := buildInfoVersion()
	return gateVersionFrom(version, bv, ok)
}
