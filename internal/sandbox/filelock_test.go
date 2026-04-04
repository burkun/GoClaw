package sandbox

import (
	"sync"
	"testing"
)

func TestGetFileOperationLock(t *testing.T) {
	// Clear any existing locks before test
	ClearFileOperationLocks()

	// Test that getting a lock for the same (sandboxID, path) returns the same lock
	lock1 := GetFileOperationLock("sandbox1", "/path/to/file")
	lock2 := GetFileOperationLock("sandbox1", "/path/to/file")
	if lock1 != lock2 {
		t.Error("expected same lock for same sandboxID and path")
	}

	// Test that different paths get different locks
	lock3 := GetFileOperationLock("sandbox1", "/path/to/other")
	if lock1 == lock3 {
		t.Error("expected different locks for different paths")
	}

	// Test that different sandbox IDs get different locks
	lock4 := GetFileOperationLock("sandbox2", "/path/to/file")
	if lock1 == lock4 {
		t.Error("expected different locks for different sandboxIDs")
	}
}

func TestWithFileLock(t *testing.T) {
	ClearFileOperationLocks()

	// Test that WithFileLock executes the function
	executed := false
	err := WithFileLock("sandbox1", "/path/to/file", func() error {
		executed = true
		return nil
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !executed {
		t.Error("expected function to be executed")
	}
}

func TestWithFileLock_Concurrency(t *testing.T) {
	ClearFileOperationLocks()

	// Test that concurrent access is properly serialized
	var counter int
	var wg sync.WaitGroup
	numGoroutines := 10
	incrementsPerGoroutine := 100

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGoroutine; j++ {
				_ = WithFileLock("sandbox1", "/path/to/file", func() error {
					counter++
					return nil
				})
			}
		}()
	}
	wg.Wait()

	expected := numGoroutines * incrementsPerGoroutine
	if counter != expected {
		t.Errorf("expected counter to be %d, got %d", expected, counter)
	}
}

func TestClearFileOperationLocks(t *testing.T) {
	// Create some locks
	_ = GetFileOperationLock("sandbox1", "/path/to/file")
	_ = GetFileOperationLock("sandbox2", "/path/to/other")

	// Clear all locks
	ClearFileOperationLocks()

	// Verify that the lock map is empty (indirectly via getting a new lock)
	// After clear, getting a lock should return a new lock
	// We can't directly check the map size, but we can verify functionality
	lock1 := GetFileOperationLock("sandbox1", "/path/to/file")
	if lock1 == nil {
		t.Error("expected non-nil lock after clear")
	}
}
