package cli

import (
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
)

// TestDevOptOutWarning pins #16: the "ignoring unparseable FORMWORK_ALLOW_DEV"
// notice must fire only where the opt-out could actually change the outcome —
// the dev-build path (trusted gate version "dev"). On a trusted release the
// opt-out is never consulted, so an unparseable value is irrelevant and must
// stay silent, the same "irrelevant opt-out prints nothing" property already
// guaranteed for an absent engine: field.
func TestDevOptOutWarning(t *testing.T) {
	cases := []struct {
		name           string
		trustedVersion string
		allowDevRaw    string
		wantWarn       bool
	}{
		{"trusted release, unparseable value stays silent", "0.3.0", "yes", false},
		{"trusted release, valid value stays silent", "0.3.0", "1", false},
		{"trusted release, empty stays silent", "0.3.0", "", false},
		{"dev build, unparseable value warns", "dev", "yes", true},
		{"dev build, valid true is silent", "dev", "1", false},
		{"dev build, valid false is silent", "dev", "false", false},
		{"dev build, empty is silent", "dev", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := devOptOutWarning(tc.trustedVersion, tc.allowDevRaw)
			if (got != "") != tc.wantWarn {
				t.Fatalf("devOptOutWarning(%q, %q) = %q, wantWarn = %v", tc.trustedVersion, tc.allowDevRaw, got, tc.wantWarn)
			}
			if tc.wantWarn && !strings.Contains(got, tc.allowDevRaw) {
				t.Fatalf("warning should quote the offending value %q: %q", tc.allowDevRaw, got)
			}
		})
	}
}

func mustConstraint(t *testing.T, s string) *semver.Constraints {
	t.Helper()
	c, err := semver.NewConstraint(s)
	if err != nil {
		t.Fatalf("bad constraint %q: %v", s, err)
	}
	return c
}

func TestCheckEngine(t *testing.T) {
	c := mustConstraint(t, ">=0.2.0")
	cases := []struct {
		name           string
		raw            string
		constraint     *semver.Constraints
		rawVersion     string
		trustedVersion string
		allowDev       bool
		wantWarn       bool
		wantErr        bool
	}{
		{"no engine field", "", nil, "dev", "dev", false, false, false},
		{"satisfied", ">=0.2.0", c, "v0.3.0", "v0.3.0", false, false, false},
		{"exact boundary satisfied", ">=0.2.0", c, "v0.2.0", "v0.2.0", false, false, false},
		{"unsatisfied", ">=0.2.0", c, "v0.1.0", "v0.1.0", false, false, true},
		{"invalid trusted version errors", ">=0.2.0", c, "not-semver", "not-semver", false, false, true},
		{"prerelease does not satisfy plain constraint", ">=0.2.0", c, "v1.0.0-rc.1", "v1.0.0-rc.1", false, false, true},

		// trustedVersion == "dev", rawVersion == "dev" too (truly unidentifiable):
		// allowDev governs.
		{"dev fails closed", ">=0.2.0", c, "dev", "dev", false, false, true},
		{"dev opt-out warns and proceeds", ">=0.2.0", c, "dev", "dev", true, true, false},

		// trustedVersion == "dev" but rawVersion is a stamped-but-untrusted
		// string that parses as semver (dirty build metadata — a prerelease-free
		// form, so Masterminds' plain-constraint prerelease exclusion doesn't
		// mask the comparison): hard evidence of failure is refused
		// unconditionally, even with allowDev set — the opt-out is for binaries
		// we cannot identify, not for discarding one we can identify as too old.
		{"untrusted raw version failing constraint refused even with allowDev", ">=0.2.0", c, "v0.1.0+dirty", "dev", true, false, true},
		{"untrusted raw version failing constraint refused without allowDev", ">=0.2.0", c, "v0.1.0+dirty", "dev", false, false, true},

		// trustedVersion == "dev", rawVersion parses but happens to satisfy the
		// constraint: untrusted evidence never grants a pass on its own, so
		// this still falls through to the allowDev/fail-closed branches.
		{"untrusted raw version satisfying constraint still warns with allowDev", ">=0.2.0", c, "v0.3.0+dirty", "dev", true, true, false},
		{"untrusted raw version satisfying constraint still fails closed without allowDev", ">=0.2.0", c, "v0.3.0+dirty", "dev", false, false, true},

		// trustedVersion == "dev", rawVersion does not even parse as semver:
		// no hard evidence either way, so allowDev governs as before.
		{"unparseable raw version, dev opt-out warns", ">=0.2.0", c, "not-semver-either", "dev", true, true, false},
		{"unparseable raw version, fails closed", ">=0.2.0", c, "not-semver-either", "dev", false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			warn, err := checkEngine(tc.raw, tc.constraint, tc.rawVersion, tc.trustedVersion, tc.allowDev)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if (warn != "") != tc.wantWarn {
				t.Fatalf("warn = %q, wantWarn = %v", warn, tc.wantWarn)
			}
		})
	}
}
