// Package sandbox provides file operation locks to prevent concurrent write conflicts.
// This mirrors deer-flow's file_operation_lock.py implementation.
//
// This package provides two types of locks:
// 1. In-process locks (sync.Mutex) - fast but only work within a single process
// 2. Cross-process locks (flock) - work across multiple processes but slower
//
// For best performance, use in-process locks when you know all file operations
// are within the same process. Use cross-process locks when multiple processes
// may access the same files.
package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"goclaw/pkg/errors"
)

// FileLockMode determines the lock implementation to use.
type FileLockMode int

const (
	// LockModeInProcess uses sync.Mutex - fast but only works within a single process.
	LockModeInProcess FileLockMode = iota

	// LockModeCrossProcess uses flock - works across multiple processes.
	LockModeCrossProcess
)

// fileOperationLocks stores locks keyed by (sandboxID, path).
// Each lock protects concurrent access to the same file within a sandbox.
var (
	fileOperationLocks      = make(map[fileLockKey]*sync.Mutex)
	fileOperationLocksGuard sync.Mutex
)

// fileLockKey is the composite key for file operation locks.
type fileLockKey struct {
	sandboxID string
	path      string
}

// GetFileOperationLock returns a mutex lock for the given sandbox and path.
// If no lock exists for this (sandboxID, path) pair, one is created.
// The same lock is returned for subsequent calls with the same arguments.
//
// Usage:
//
//	lock := sandbox.GetFileOperationLock(sandbox.ID(), path)
//	lock.Lock()
//	defer lock.Unlock()
//	// perform file operation
func GetFileOperationLock(sandboxID, path string) *sync.Mutex {
	key := fileLockKey{sandboxID: sandboxID, path: path}
	fileOperationLocksGuard.Lock()
	defer fileOperationLocksGuard.Unlock()

	lock, exists := fileOperationLocks[key]
	if !exists {
		lock = &sync.Mutex{}
		fileOperationLocks[key] = lock
	}
	return lock
}

// WithFileLock acquires the file lock for the given sandbox and path,
// executes the provided function, and releases the lock.
// This is a convenience wrapper for the common lock/do/unlock pattern.
//
// Usage:
//
//	err := sandbox.WithFileLock(sandbox.ID(), path, func() error {
//	    return sandbox.WriteFile(ctx, path, content, false)
//	})
func WithFileLock(sandboxID, path string, fn func() error) error {
	lock := GetFileOperationLock(sandboxID, path)
	lock.Lock()
	defer lock.Unlock()
	return fn()
}

// ClearFileOperationLocks removes all stored locks.
// This is primarily useful for testing to reset state between tests.
func ClearFileOperationLocks() {
	fileOperationLocksGuard.Lock()
	defer fileOperationLocksGuard.Unlock()
	fileOperationLocks = make(map[fileLockKey]*sync.Mutex)
}

// CrossProcessFileLock implements cross-process file locking using flock.
// This is the Go equivalent of DeerFlow's filelock.FileLock.
type CrossProcessFileLock struct {
	file    *os.File
	lockDir string
}

// NewCrossProcessFileLock creates a new cross-process file lock.
// lockDir is the directory where lock files will be created.
// If lockDir doesn't exist, it will be created.
func NewCrossProcessFileLock(lockDir string) (*CrossProcessFileLock, error) {
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		return nil, errors.WrapInternalError(err, "create lock directory")
	}
	return &CrossProcessFileLock{lockDir: lockDir}, nil
}

// Acquire acquires an exclusive lock on the given file path.
// It creates a lock file in the lock directory and uses flock to acquire the lock.
// The lock is automatically released when the returned unlock function is called.
//
// This method blocks until the lock is acquired or context is cancelled.
func (cpl *CrossProcessFileLock) Acquire(ctx context.Context, filePath string) (unlock func(), err error) {
	// Create lock file path
	lockFileName := filepath.Join(cpl.lockDir, filepath.Base(filePath)+".lock")

	// Open or create lock file
	file, err := os.OpenFile(lockFileName, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, errors.WrapInternalError(err, "open lock file")
	}

	// Try to acquire lock with context support
	acquired := make(chan error, 1)
	go func() {
		acquired <- cpl.tryAcquireFileLock(file)
	}()

	select {
	case err := <-acquired:
		if err != nil {
			file.Close()
			return nil, errors.WrapInternalError(err, "acquire lock")
		}
		// Lock acquired successfully
		return func() {
			_ = cpl.releaseFileLock(file) // 清理操作，忽略错误
			file.Close()
		}, nil
	case <-ctx.Done():
		file.Close()
		return nil, ctx.Err()
	}
}

// AcquireWithTimeout acquires an exclusive lock with a timeout.
// This is a convenience wrapper around Acquire with a timeout context.
func (cpl *CrossProcessFileLock) AcquireWithTimeout(filePath string, timeout time.Duration) (unlock func(), err error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return cpl.Acquire(ctx, filePath)
}

// tryAcquireFileLock attempts to acquire an exclusive lock on the file.
// Platform-specific implementation is in filelock_unix.go and filelock_windows.go.
func (cpl *CrossProcessFileLock) tryAcquireFileLock(file *os.File) error {
	return tryAcquireFileLockPlatform(file)
}

// releaseFileLock releases the lock on the file.
// Platform-specific implementation is in filelock_unix.go and filelock_windows.go.
func (cpl *CrossProcessFileLock) releaseFileLock(file *os.File) error {
	return releaseFileLockPlatform(file)
}
