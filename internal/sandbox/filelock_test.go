package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCrossProcessFileLock_Basic(t *testing.T) {
	tmp := t.TempDir()
	lockDir := filepath.Join(tmp, "locks")
	
	cpl, err := NewCrossProcessFileLock(lockDir)
	require.NoError(t, err)
	
	testFile := filepath.Join(tmp, "test.txt")
	
	// Acquire lock
	unlock, err := cpl.Acquire(context.Background(), testFile)
	require.NoError(t, err)
	
	// Perform file operation
	err = os.WriteFile(testFile, []byte("hello"), 0644)
	require.NoError(t, err)
	
	// Release lock
	unlock()
	
	// Verify file was written
	data, err := os.ReadFile(testFile)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
}

func TestCrossProcessFileLock_Timeout(t *testing.T) {
	tmp := t.TempDir()
	lockDir := filepath.Join(tmp, "locks")
	
	cpl, err := NewCrossProcessFileLock(lockDir)
	require.NoError(t, err)
	
	testFile := filepath.Join(tmp, "test.txt")
	
	// Acquire lock in first goroutine
	unlock1, err := cpl.Acquire(context.Background(), testFile)
	require.NoError(t, err)
	defer unlock1()
	
	// Try to acquire the same lock with timeout - should fail
	_, err = cpl.AcquireWithTimeout(testFile, 100*time.Millisecond)
	assert.Error(t, err, "should timeout when lock is held by another")
	// The error can be either context deadline or flock "resource temporarily unavailable"
	assert.Contains(t, err.Error(), "resource temporarily unavailable")
}

func TestCrossProcessFileLock_Sequential(t *testing.T) {
	tmp := t.TempDir()
	lockDir := filepath.Join(tmp, "locks")
	
	cpl, err := NewCrossProcessFileLock(lockDir)
	require.NoError(t, err)
	
	testFile := filepath.Join(tmp, "test.txt")
	
	// First lock
	unlock1, err := cpl.Acquire(context.Background(), testFile)
	require.NoError(t, err)
	unlock1()
	
	// Second lock on same file should succeed after first is released
	unlock2, err := cpl.Acquire(context.Background(), testFile)
	require.NoError(t, err)
	unlock2()
}

func TestCrossProcessFileLock_MultipleFiles(t *testing.T) {
	tmp := t.TempDir()
	lockDir := filepath.Join(tmp, "locks")
	
	cpl, err := NewCrossProcessFileLock(lockDir)
	require.NoError(t, err)
	
	file1 := filepath.Join(tmp, "file1.txt")
	file2 := filepath.Join(tmp, "file2.txt")
	
	// Acquire locks on different files concurrently - should succeed
	unlock1, err := cpl.Acquire(context.Background(), file1)
	require.NoError(t, err)
	
	unlock2, err := cpl.Acquire(context.Background(), file2)
	require.NoError(t, err)
	
	// Both locks should be held simultaneously
	unlock1()
	unlock2()
}
