//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly || illumos

// The constraint is an explicit allowlist and not `unix`, because `unix` is
// satisfied by three platforms whose syscall package does not carry this call:
// solaris has no LOCK_EX, and aix has neither Flock nor LOCK_EX (illumos, which
// also sets the `solaris` build tag, does carry both — which is why the
// exclusion cannot be spelled `unix && !solaris`). Under `unix` those three did
// not build at all: the fallback in heavygate_flock_other.go exists precisely to
// keep every non-flock platform compiling, and a constraint that claims them
// while the compensating file disclaims them leaves them covered by neither.
// TestFlockBuildTagsCompileOnEveryClaimedPlatform builds the package for each.

package engine

import (
	"errors"
	"os"
	"syscall"
)

// tryLockFile takes an exclusive, non-blocking flock(2) on f. It reports
// whether the lock was taken; a slot already held by another descriptor
// (in this process or any other) is (false, nil), not an error.
//
// The lock is released by closing f — the caller's release does that — and
// also by the kernel if this process dies without closing anything, which is
// the property the whole gate is built on.
func tryLockFile(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, syscall.EWOULDBLOCK):
		return false, nil
	default:
		return false, err
	}
}
