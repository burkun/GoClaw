//go:build windows
// +build windows

package sandbox

import (
	"errors"
	"os"
	"syscall"
	"unsafe"

	goclawerrors "goclaw/pkg/errors"
)

var (
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	lockFileExProc = kernel32.NewProc("LockFileEx")
	unlockFileProc = kernel32.NewProc("UnlockFile")
)

const (
	LOCKFILE_EXCLUSIVE_LOCK   = 0x00000002
	LOCKFILE_FAIL_IMMEDIATELY = 0x00000001
)

// tryAcquireFileLockPlatform acquires an exclusive lock on Windows using LockFileEx.
func tryAcquireFileLockPlatform(file *os.File) error {
	// Overlapped structure for LockFileEx
	var overlapped syscall.Overlapped

	// Lock the entire file (0 to EOF)
	// dwFlags: LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY
	ret, _, err := lockFileExProc.Call(
		uintptr(file.Fd()),
		uintptr(LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY),
		0,          // reserved
		0xFFFFFFFF, // lock entire file (low 32 bits)
		0xFFFFFFFF, // lock entire file (high 32 bits)
		uintptr(unsafe.Pointer(&overlapped)),
	)

	if ret == 0 {
		return goclawerrors.WrapInternalError(err, "LockFileEx failed")
	}
	return nil
}

// isLockContentionErrorPlatform returns true when the lock is held by another process.
// On Windows, LockFileEx with LOCKFILE_FAIL_IMMEDIATELY returns ERROR_LOCK_VIOLATION.
func isLockContentionErrorPlatform(err error) bool {
	var gerr *goclawerrors.Error
	if errors.As(err, &gerr) {
		cause := gerr.Unwrap()
		if errno, ok := cause.(syscall.Errno); ok {
			const ERROR_LOCK_VIOLATION syscall.Errno = 33
			return errno == ERROR_LOCK_VIOLATION
		}
	}
	return false
}

// releaseFileLockPlatform releases the lock on Windows.
func releaseFileLockPlatform(file *os.File) error {
	// Unlock the entire file
	ret, _, err := unlockFileProc.Call(
		uintptr(file.Fd()),
		0,          // start offset (low 32 bits)
		0,          // start offset (high 32 bits)
		0xFFFFFFFF, // length (low 32 bits)
		0xFFFFFFFF, // length (high 32 bits)
	)

	if ret == 0 {
		return goclawerrors.WrapInternalError(err, "UnlockFile failed")
	}
	return nil
}
