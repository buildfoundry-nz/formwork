package vcs

import "os"

// SetSameFileForTest replaces the directory-identity comparison and returns a
// function restoring it.
//
// The seam exists because os.SameFile cannot be made to fail from outside: on
// unix it is a bare `Dev == Dev && Ino == Ino` with no error path, and a
// filesystem that cannot distinguish directories answers "same" for every
// pair. That degeneracy makes an identity-only guard silently inert rather
// than fail-closed, which is precisely the property the guard must not have —
// and the only way to pin it is to force the degenerate answer.
func SetSameFileForTest(fn func(a, b os.FileInfo) bool) (restore func()) {
	prev := sameFile
	sameFile = fn
	return func() { sameFile = prev }
}

// ParseWorktreesForTest exposes the `worktree list --porcelain -z` parser so a
// test can present bytes git will not produce on demand.
//
// The seam exists for the fail-closed direction specifically: parseWorktrees
// must return an error on a record it cannot read, and no way was found to make
// git emit one — `worktree list --porcelain -z` opens every record with
// `worktree <path>`, and each state hooks_test.go drives git into (main, linked,
// bare, locked, prunable, a newline in the path) obeys that. That is the basis
// for the seam and not a proof over every git invocation; the malformed records
// are hand-written bytes. Without this the only provable half is the happy path,
// which is the half that was never in doubt.
func ParseWorktreesForTest(out string) ([]Worktree, error) { return parseWorktrees(out) }

// SetRepoIdentityForTest replaces the question the environment guard asks git
// and returns a function restoring it.
//
// The seam exists because the failure it pins cannot be produced from outside:
// `git rev-parse` ECHOES an option it does not recognise and exits 0, so on a git
// missing a flag this package passes, BOTH runs return the same non-answer, the
// comparison calls that agreement, and the guard goes silently inert. No git on
// this machine reproduces that — it understands every option the real question
// uses — and pinning the behaviour by installing an old git is not something a
// test can do. Driving one unknown option through here reproduces exactly the
// shape an old git would present, which is what validateIdentity refuses.
func SetRepoIdentityForTest(args []string, parts int) (restore func()) {
	prev := repoIdentity
	repoIdentity = identityQuestion{args: args, parts: parts}
	return func() { repoIdentity = prev }
}
