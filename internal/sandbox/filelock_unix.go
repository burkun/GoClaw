//go:build !windows
// +build !windows

package sandbox

import (
	"os"

	"golang.org/x/sys/unix"

	"goclaw/pkg/errors"
)

// tryAcquireFileLockPlatform acquires an exclusive lock on Unix/Linux/macOS using flock.
func tryAcquireFileLockPlatform(file *os.File) error {
	// LOCK_EX: exclusive lock
	// LOCK_NB: non-blocking (we handle blocking in the caller)
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err != nil {
		return errors.WrapInternalError(err, "flock")
	}
	return nil
}

// releaseFileLockPlatform releases the lock on Unix/Linux/macOS.
func releaseFileLockPlatform(file *os.File) error {
	err := unix.Flock(int(file.Fd()), unix.LOCK_UN)
	if err != nil {
		return errors.WrapInternalError(err, "unlock flock")
	}
	return nil
}
