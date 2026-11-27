//go:build linux || darwin

package cputime

import (
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const Supported = true

// Thread reads the calling OS thread's user and system CPU. The caller must
// hold runtime.LockOSThread so the goroutine cannot move threads between reads.
func Thread() (time.Duration, bool) {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_THREAD_CPUTIME_ID, &ts); err != nil {
		return 0, false
	}
	return time.Duration(ts.Sec)*time.Second + time.Duration(ts.Nsec), true
}

// Process reads user and system CPU for this process and children it waited
// for, so waited-for descendants are included exactly once.
func Process() (time.Duration, bool) {
	self, ok := rusage(syscall.RUSAGE_SELF)
	if !ok {
		return 0, false
	}
	children, ok := rusage(syscall.RUSAGE_CHILDREN)
	if !ok {
		return 0, false
	}
	return self + children, true
}

func rusage(who int) (time.Duration, bool) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(who, &ru); err != nil {
		return 0, false
	}
	user := time.Duration(ru.Utime.Sec)*time.Second + time.Duration(ru.Utime.Usec)*time.Microsecond
	system := time.Duration(ru.Stime.Sec)*time.Second + time.Duration(ru.Stime.Usec)*time.Microsecond
	return user + system, true
}
