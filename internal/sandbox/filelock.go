// Package sandbox provides file operation locks to prevent concurrent write conflicts.
// This mirrors deer-flow's file_operation_lock.py implementation.
package sandbox

import (
	"sync"
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
