// +build windows

package sandbox

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	lockFileExProc  = kernel32.NewProc("LockFileEx")
	unlockFileProc  = kernel32.NewProc("UnlockFile")
)

const (
	LOCKFILE_EXCLUSIVE_LOCK = 0x00000002
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
		0, // reserved
		0xFFFFFFFF, // lock entire file (low 32 bits)
		0xFFFFFFFF, // lock entire file (high 32 bits)
		uintptr(unsafe.Pointer(&overlapped)),
	)
	
	if ret == 0 {
		return fmt.Errorf("LockFileEx failed: %w", err)
	}
	return nil
}

// releaseFileLockPlatform releases the lock on Windows.
func releaseFileLockPlatform(file *os.File) error {
	// Unlock the entire file
	ret, _, err := unlockFileProc.Call(
		uintptr(file.Fd()),
		0, // start offset (low 32 bits)
		0, // start offset (high 32 bits)
		0xFFFFFFFF, // length (low 32 bits)
		0xFFFFFFFF, // length (high 32 bits)
	)
	
	if ret == 0 {
		return fmt.Errorf("UnlockFile failed: %w", err)
	}
	return nil
}
