package cli

import "testing"

func TestRawVersion(t *testing.T) {
	cases := []struct {
		name         string
		ldflags      string
		buildVersion string
		haveBuild    bool
		want         string
	}{
		{"ldflags set wins", "v0.2.0", "(devel)", true, "v0.2.0"},
		{"ldflags dev falls through to build", "dev", "v0.3.1", true, "v0.3.1"},
		{"ldflags empty falls through to build", "", "v0.3.1", true, "v0.3.1"},
		{"ldflags wins even when unreleased", "v0.3.0+dirty", "v1.0.0", true, "v0.3.0+dirty"},
		{"build (devel) is dev", "dev", "(devel)", true, "dev"},
		{"build empty is dev", "dev", "", true, "dev"},
		{"no build info is dev", "dev", "", false, "dev"},
		{"ldflags empty, no build info is dev", "", "", false, "dev"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := rawVersion(c.ldflags, c.buildVersion, c.haveBuild)
			if got != c.want {
				t.Fatalf("rawVersion(%q, %q, %v) = %q, want %q",
					c.ldflags, c.buildVersion, c.haveBuild, got, c.want)
			}
		})
	}
}

func TestTrustedVersion(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"plain release", "v0.2.0", "v0.2.0"},
		{"plain release", "v1.2.3", "v1.2.3"},
		{"plain release", "v10.20.30", "v10.20.30"},
		{"incompatible is a real release", "v2.0.0+incompatible", "v2.0.0+incompatible"},
		{"prerelease is a real release", "v1.0.0-rc.1", "v1.0.0-rc.1"},
		{"no-base pseudo-version is dev", "v0.0.0-20260723150405-abcdef123456", "dev"},
		{"tagged-base pseudo-version is dev", "v1.2.4-0.20260723150405-abcdef123456", "dev"},
		{"prerelease-base pseudo-version is dev", "v1.2.3-rc.0.20260723150405-abcdef123456", "dev"},
		{"dirty pseudo-version (+dirty) is dev", "v0.0.0-20260723150405-abcdef123456+dirty", "dev"},
		{"dirty tagged version (+dirty) is dev", "v1.2.3+dirty", "dev"},
		{"dirty tagged version (-dirty) is dev", "v1.2.3-dirty", "dev"},
		{"describe form ahead of tag is dev", "v0.3.0-3-gabc1234", "dev"},
		{"describe form ahead of tag, dirty, is dev", "v0.3.0-3-gabc1234-dirty", "dev"},
		{"describe form with a 4-hex abbreviated hash is dev", "v0.3.0-3-gabc1", "dev"},
		// GoReleaser's {{ .Version }} template value — what .goreleaser.yaml
		// stamps into every released binary — is the tag with its leading "v"
		// stripped (tag v0.2.0 stamps "0.2.0"). A "v" prefix is NOT required:
		// this is exactly the form a real release carries, and it must be
		// trusted or every released binary fails engine gating.
		{"no v prefix (the GoReleaser release stamp form) is trusted", "1.2.3", "1.2.3"},
		{"dirty tagged version (+dirty) no v prefix is dev", "1.2.3+dirty", "dev"},
		{"dirty with trailing build metadata (-dirty+build.7) is dev", "v0.3.0-dirty+build.7", "dev"},
		{"dirty with trailing build metadata (+dirty.1) is dev", "v0.3.0+dirty.1", "dev"},
		{"not semver at all is dev", "not-a-version", "dev"},
		{"dev sentinel is dev", "dev", "dev"},
		{"empty is dev", "", "dev"},
		{"(devel) is dev", "(devel)", "dev"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := trustedVersion(c.raw)
			if got != c.want {
				t.Fatalf("trustedVersion(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

// TestGateVersionFrom binds gateVersionFrom (and thus gateVersion) directly
// to a non-dev, release-form input. Without this, a change that silently
// reverts gateVersion to rawVersion — e.g. dropping the trustedVersion call
// entirely — leaves the rest of the suite green, because nothing else
// exercises gateVersion(From) with a version that isn't already "dev". That
// gap is exactly how the release-breaking "v" prefix regression shipped
// undetected: rawVersion, trustedVersion, and annotate were all tested in
// isolation, but nothing pinned gateVersion's own behavior on a real release
// stamp.
func TestGateVersionFrom(t *testing.T) {
	cases := []struct {
		name         string
		ldflags      string
		buildVersion string
		haveBuild    bool
		want         string
	}{
		// The GoReleaser {{ .Version }} form — no leading "v" — is exactly what
		// .goreleaser.yaml stamps into every released binary. This case is the
		// regression lock for finding 1: do not "tidy" it away.
		{"GoReleaser release stamp form (no v) is trusted", "0.2.0", "", false, "0.2.0"},
		{"release stamp form (with v) is trusted", "v0.2.0", "", false, "v0.2.0"},
		{"dev sentinel is dev", "dev", "", false, "dev"},
		{"pseudo-version is dev", "0.0.0-20260723150405-abcdef123456", "", false, "dev"},
		{"dirty (-dirty) is dev", "v0.3.0-dirty", "", false, "dev"},
		{"dirty (+dirty) is dev", "v0.3.0+dirty", "", false, "dev"},
		{"dirty with trailing metadata (-dirty+build.7) is dev", "v0.3.0-dirty+build.7", "", false, "dev"},
		{"dirty with trailing metadata (+dirty.1) is dev", "v0.3.0+dirty.1", "", false, "dev"},
		{"dirty, no v prefix, is dev", "0.3.0-dirty", "", false, "dev"},
		{"describe form ahead of tag is dev", "v0.3.0-3-gabc1234", "", false, "dev"},
		{"describe form with a 4-hex hash is dev", "v0.3.0-3-gabc1", "", false, "dev"},
		{"build-info release version (no ldflags) is trusted", "dev", "v1.4.0", true, "v1.4.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := gateVersionFrom(c.ldflags, c.buildVersion, c.haveBuild)
			if got != c.want {
				t.Fatalf("gateVersionFrom(%q, %q, %v) = %q, want %q",
					c.ldflags, c.buildVersion, c.haveBuild, got, c.want)
			}
		})
	}
}

func TestDisplayVersionAnnotatesUnreleased(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"released version displays bare", "v1.2.3", "v1.2.3"},
		{"incompatible release displays bare", "v2.0.0+incompatible", "v2.0.0+incompatible"},
		{"dirty stamp keeps the real string, annotated", "v0.3.0+dirty", "v0.3.0+dirty (unreleased build)"},
		{"dirty describe form keeps the real string, annotated", "v0.3.0-dirty", "v0.3.0-dirty (unreleased build)"},
		{"describe-ahead-of-tag keeps the real string, annotated", "v0.3.0-3-gabc1234", "v0.3.0-3-gabc1234 (unreleased build)"},
		{"pseudo-version keeps the real string, annotated", "v0.0.0-20260723150405-abcdef123456", "v0.0.0-20260723150405-abcdef123456 (unreleased build)"},
		{"dev sentinel is bare dev, not annotated", "dev", "dev"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := annotate(c.raw)
			if got != c.want {
				t.Fatalf("annotate(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}
