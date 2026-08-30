//go:build !(linux || darwin || freebsd || openbsd || netbsd || dragonfly || illumos)

package engine

import (
	"errors"
	"os"
)

// tryLockFile has no implementation where flock(2) is not in the syscall
// package. .goreleaser.yaml builds linux and darwin only, so this file exists to
// keep the package compiling elsewhere rather than to serve a shipped platform
// — and it returns an error rather than a silent (false, nil) so the gate takes
// its disclosed fail-open path instead of spinning until the deadline against
// slots it can never take.
//
// Its constraint is the exact negation of the flock file's allowlist, so every
// GOOS has exactly one implementation: windows and js reach it because they are
// not unix at all, and solaris and aix reach it because their syscall package is
// missing the call even though they are.
func tryLockFile(*os.File) (bool, error) {
	return false, errors.ErrUnsupported
}
