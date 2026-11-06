//go:build !linux && !darwin

package cputime

import "time"

const Supported = false

func Thread() (time.Duration, bool) {
	return 0, false
}

func Process() (time.Duration, bool) {
	return 0, false
}
