//go:build !windows
// +build !windows

package sandbox

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"

	goclawerrors "goclaw/pkg/errors"
)

// tryAcquireFileLockPlatform acquires an exclusive lock on Unix/Linux/macOS using flock.
func tryAcquireFileLockPlatform(file *os.File) error {
	// LOCK_EX: exclusive lock
	// LOCK_NB: non-blocking (we handle blocking in the caller)
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err != nil {
		return goclawerrors.WrapInternalError(err, "flock")
	}
	return nil
}

// isLockContentionErrorPlatform returns true when errno indicates the lock
// is already held by another process (EAGAIN / EWOULDBLOCK on Unix).
func isLockContentionErrorPlatform(err error) bool {
	var gerr *goclawerrors.Error
	if errors.As(err, &gerr) {
		cause := gerr.Unwrap()
		if errno, ok := cause.(syscall.Errno); ok {
			return errno == syscall.EAGAIN || errno == syscall.EWOULDBLOCK
		}
	}
	return false
}

// releaseFileLockPlatform releases the lock on Unix/Linux/macOS.
func releaseFileLockPlatform(file *os.File) error {
	err := unix.Flock(int(file.Fd()), unix.LOCK_UN)
	if err != nil {
		return goclawerrors.WrapInternalError(err, "unlock flock")
	}
	return nil
}
