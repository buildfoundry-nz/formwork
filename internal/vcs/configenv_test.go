package vcs_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// huskyRepo is a repository whose OWN config names a hooks directory, which is
// the state every question below is asked over: the ambient environment has
// something to disagree with only where the repository already said something.
func huskyRepo(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)
	run(t, dir, "config", "core.hooksPath", ".husky")
	return dir
}

// THE VARIABLE THAT DEFEATS A LIST. GIT_CONFIG_PARAMETERS is git's own `-c`
// propagation channel: it appears in neither git(1)'s ENVIRONMENT section nor
// git-config(1), so no sweep of the documentation produces it. Measured on git
// 2.50.1, over a repository whose local core.hooksPath is `.husky`, it makes
// `rev-parse --git-path hooks` answer `.formwork/hooks`.
//
// The measurement is what this test asserts: not that the variable is known, but
// that asking git twice reports the disagreement without anyone having named it.
func TestConfigEnvEffectFindsAVariableNoListNames(t *testing.T) {
	dir := huskyRepo(t)
	t.Setenv("GIT_CONFIG_PARAMETERS", "'core.hooksPath'='.formwork/hooks'")

	eff, err := vcs.MeasureConfigEnv(dir, vcs.HooksPathQuestion)
	if err != nil {
		t.Fatal(err)
	}
	if !eff.Changed {
		t.Fatalf("GIT_CONFIG_PARAMETERS moved git's answer and the measurement missed it: %+v", eff)
	}
	if !strings.Contains(eff.Ambient, ".formwork/hooks") || !strings.Contains(eff.Scrubbed, ".husky") {
		t.Fatalf("the two answers are the evidence and they are wrong: %+v", eff)
	}
	if !slices.Contains(eff.Removed, "GIT_CONFIG_PARAMETERS") {
		t.Errorf("Removed = %v, want it to name GIT_CONFIG_PARAMETERS — the operator has to know what to unset", eff.Removed)
	}
}

// The other direction, and the one a presence test gets wrong: these are set,
// git ignores them, and a guard that fires on them refuses a healthy repository.
// Measured on git 2.50.1 — each row leaves `rev-parse --git-path hooks`
// byte-identical.
func TestConfigEnvEffectIsSilentWhereGitIgnoresTheVariable(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"GIT_CONFIG_COUNT", "0"},              // zero keys follow, so nothing is injected
		{"GIT_CONFIG_NOSYSTEM", ""},            // set-but-empty: git reads it as off
		{"GIT_CONFIG_KEY_0", "core.hooksPath"}, // inert without a COUNT to enumerate it
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := huskyRepo(t)
			t.Setenv(tc.name, tc.value)

			eff, err := vcs.MeasureConfigEnv(dir, vcs.HooksPathQuestion)
			if err != nil {
				t.Fatal(err)
			}
			if eff.Changed {
				t.Fatalf("%s=%q changes nothing git does, but the measurement calls it a difference: %+v", tc.name, tc.value, eff)
			}
		})
	}
}

// GIT_CONFIG_COUNT with keys behind it is the family member the old guard was
// built for, and it must still be caught — by effect now rather than by name.
func TestConfigEnvEffectCatchesInjectedConfigKeys(t *testing.T) {
	dir := huskyRepo(t)
	for k, v := range map[string]string{
		"GIT_CONFIG_COUNT":   "1",
		"GIT_CONFIG_KEY_0":   "core.hooksPath",
		"GIT_CONFIG_VALUE_0": ".formwork/hooks",
	} {
		t.Setenv(k, v)
	}

	eff, err := vcs.MeasureConfigEnv(dir, vcs.HooksPathQuestion)
	if err != nil {
		t.Fatal(err)
	}
	if !eff.Changed {
		t.Fatalf("an injected core.hooksPath went unmeasured: %+v", eff)
	}
}

// THE ANSWER IS PER-QUESTION, which is why this takes one. The same environment
// that moves where git runs hooks leaves the shared git directory exactly where
// it was — measured on git 2.50.1 — so a single "the environment is dirty"
// verdict would be wrong in both directions: it would refuse questions the
// environment cannot reach, and pass ones it can.
func TestConfigEnvEffectIsAskedPerQuestion(t *testing.T) {
	dir := huskyRepo(t)
	t.Setenv("GIT_CONFIG_PARAMETERS", "'core.hooksPath'='.formwork/hooks'")

	moved, err := vcs.MeasureConfigEnv(dir, vcs.HooksPathQuestion)
	if err != nil {
		t.Fatal(err)
	}
	still, err := vcs.MeasureConfigEnv(dir, vcs.CommonDirQuestion)
	if err != nil {
		t.Fatal(err)
	}
	if !moved.Changed || still.Changed {
		t.Fatalf("one environment, two questions: hooks path changed=%v, shared git directory changed=%v — want true, false", moved.Changed, still.Changed)
	}
}

// The hatch turns off the SCRUB, and this measurement is not the scrub. Nothing
// is removed from a real run's environment here — the removal happens inside the
// probe, for the length of one git call — so FORMWORK_GIT_ENV=inherit has
// nothing to honour and the disagreement is still reported.
func TestConfigEnvEffectIsMeasuredUnderTheInheritHatch(t *testing.T) {
	dir := huskyRepo(t)
	t.Setenv(vcs.GitEnvVar, "inherit")
	t.Setenv("GIT_CONFIG_PARAMETERS", "'core.hooksPath'='.formwork/hooks'")

	eff, err := vcs.MeasureConfigEnv(dir, vcs.HooksPathQuestion)
	if err != nil {
		t.Fatal(err)
	}
	if !eff.Changed {
		t.Fatalf("the hatch is about what formwork removes from a real run, not about what it measures: %+v", eff)
	}
}

// A value the hatch refuses is an error here too: the probe cannot resolve an
// environment it was told to build from a spelling formwork does not understand,
// and answering "nothing changed" would certify from a state nobody chose.
func TestConfigEnvEffectRefusesAnUnrecognisedHatchValue(t *testing.T) {
	dir := huskyRepo(t)
	t.Setenv(vcs.GitEnvVar, "yes")

	if eff, err := vcs.MeasureConfigEnv(dir, vcs.HooksPathQuestion); err == nil {
		t.Fatalf("a refused %s must not be read as a measurement: %+v", vcs.GitEnvVar, eff)
	}
}

// A question git cannot answer at all is an error, not "nothing changed": both
// runs failing is a state where the comparison was never performed, and this
// package's contract is that a git failure is a failure (vcs.go).
func TestConfigEnvEffectOutsideARepositoryIsAnError(t *testing.T) {
	if eff, err := vcs.MeasureConfigEnv(t.TempDir(), vcs.HooksPathQuestion); err == nil {
		t.Fatalf("outside a repository git answers neither run: %+v", eff)
	}
}

// The environment the probe hands git is the ambient one MINUS the config
// family, not a fresh one: dropping the rest would make the probe a different
// invocation from the run it is compared against, and every difference it then
// found would be its own.
//
// HOME is the witness, and it is the realistic one. A machine-wide
// core.hooksPath lives in $HOME/.gitconfig — a FILE, which no environment
// variable supplied and which the developer committing here has too. A probe
// that lost HOME would read git's built-in default instead, report a difference,
// and refuse every machine that has a global hooks runner configured.
func TestConfigEnvProbeKeepsTheRestOfTheEnvironment(t *testing.T) {
	dir := initRepo(t) // no local core.hooksPath: the global file is what answers
	globalGitConfig(t, "[core]\n\thooksPath = .husky\n")
	t.Setenv("GIT_CONFIG_COUNT", "0")

	eff, err := vcs.MeasureConfigEnv(dir, vcs.HooksPathQuestion)
	if err != nil {
		t.Fatal(err)
	}
	if eff.Changed || !strings.Contains(eff.Scrubbed, ".husky") {
		t.Fatalf("the probe lost the environment outside the config family, so it asked git a different question than the run it is compared against: %+v", eff)
	}
}

// globalGitConfig gives this test process its own HOME holding one git config
// file, and returns nothing: what it changes is where git looks.
//
// NOT GIT_CONFIG_GLOBAL, which is the cheaper way to write this and would make
// the fixture the thing under test — that variable is a member of the family
// this file measures, so a difference it caused would be indistinguishable from
// one the code found. XDG_CONFIG_HOME moves with it because git reads
// $XDG_CONFIG_HOME/git/config as well, so leaving the caller's would let their
// machine answer for this repository.
func globalGitConfig(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
}
