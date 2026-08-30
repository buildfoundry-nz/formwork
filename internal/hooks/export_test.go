package hooks

import "os"

// GitClientHookNamesForTest exposes the set of names git will execute as a
// hook.
//
// The seam exists because that set is a version-dependent fact about git
// extracted from githooks(5), not a constant of this package, and the failure
// direction of a missing name is silent: formwork treats a real hook as inert
// and stops reporting it. Cross-checking it against what a live `git init`
// ships needs the set itself, not a behaviour that happens to consult it.
func GitClientHookNamesForTest() map[string]bool { return gitClientHookNames }

// PreexistingHooksForTest exposes the detector behind install's D2 refusal:
// given the directory git says it will run hooks from, the hook files already
// running from git's default hooks directory, and that directory.
//
// The seam was written because the detector landed ahead of the pre-flight that
// consumes it, and that justification has expired: Install reaches it now, via
// preflight's runningHooksRefusal. It stays because the refusal is a coarser
// instrument than the detector — install's error says only that it will not
// proceed, while the tests behind this seam pin which directory came back, which
// names did, and which failures are errors rather than empty answers. Those are
// the answers a refusal is built on, and none of them is observable through it.
func PreexistingHooksForTest(root, hooksPath string) (dir string, names []string, err error) {
	return preexistingHooks(root, hooksPath)
}

// SetAccessForTest replaces the access(2) call behind the executable test and
// returns a function restoring it.
//
// See the `access` variable: the arm worth pinning is the one no chmod can
// produce — an errno that is not EACCES, meaning formwork could not perform the
// test rather than that the answer is no.
func SetAccessForTest(fn func(path string, mode uint32) error) (restore func()) {
	prev := access
	access = fn
	return func() { access = prev }
}

// SetEvalSymlinksForTest replaces the symlink resolution behind sameDirPath and
// returns a function restoring it.
//
// See the `evalSymlinks` variable: the arm worth pinning is the traversal
// failure — the state where formwork could not answer whether two spellings name
// one directory. A path that is NOT THERE is constructible without a seam and is
// not that arm; it is an honest "no", and sameDirPath answers it as one.
func SetEvalSymlinksForTest(fn func(path string) (string, error)) (restore func()) {
	prev := evalSymlinks
	evalSymlinks = fn
	return func() { evalSymlinks = prev }
}

// SetSameFileForTest replaces the directory-identity comparison behind
// writeTargetRefusal and returns a function restoring it.
//
// It is the same seam as vcs.SetSameFileForTest and exists for the same reason:
// os.SameFile has no error path on unix — a bare Dev/Ino comparison — so the
// answer a filesystem without usable file IDs gives cannot be produced from
// outside, and that answer is "same" for every pair. Forcing it is the only way
// to measure what this package's identity comparison does there, rather than
// asserting it in a comment.
func SetSameFileForTest(fn func(a, b os.FileInfo) bool) (restore func()) {
	prev := sameFile
	sameFile = fn
	return func() { sameFile = prev }
}
