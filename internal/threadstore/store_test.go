package threadstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileStore_Create(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	store, err := NewFileStore(tmpDir)
	require.NoError(t, err)

	meta := &ThreadMetadata{
		ThreadID: "thread-001",
		Title:    "Test Thread",
		Status:   "idle",
	}

	err = store.Create(meta)
	require.NoError(t, err)

	// Verify metadata is set
	assert.Equal(t, "thread-001", meta.ThreadID)
	assert.Equal(t, "Test Thread", meta.Title)
	assert.NotZero(t, meta.CreatedAt)
	assert.NotZero(t, meta.UpdatedAt)
}

func TestFileStore_Create_Duplicate(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir)
	require.NoError(t, err)

	meta := &ThreadMetadata{ThreadID: "thread-001"}

	err = store.Create(meta)
	require.NoError(t, err)

	// Create again with same ID
	err = store.Create(meta)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestFileStore_Get(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir)
	require.NoError(t, err)

	// Get non-existent thread
	_, err = store.Get("nonexistent")
	assert.Error(t, err)

	// Create and get
	meta := &ThreadMetadata{ThreadID: "thread-001", Title: "Test"}
	require.NoError(t, store.Create(meta))

	got, err := store.Get("thread-001")
	require.NoError(t, err)
	assert.Equal(t, "thread-001", got.ThreadID)
	assert.Equal(t, "Test", got.Title)
}

func TestFileStore_GetState(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir)
	require.NoError(t, err)

	// Create thread
	meta := &ThreadMetadata{ThreadID: "thread-001"}
	require.NoError(t, store.Create(meta))

	// Get state
	state, err := store.GetState("thread-001")
	require.NoError(t, err)
	assert.Equal(t, "thread-001", state.ThreadID)
}

func TestFileStore_Update(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir)
	require.NoError(t, err)

	// Create thread
	meta := &ThreadMetadata{ThreadID: "thread-001", Title: "Original"}
	require.NoError(t, store.Create(meta))

	time.Sleep(10 * time.Millisecond) // Ensure UpdatedAt changes

	// Update
	newMeta := &ThreadMetadata{
		ThreadID: "thread-001",
		Title:    "Updated",
		Status:   "busy",
	}
	err = store.Update("thread-001", newMeta)
	require.NoError(t, err)

	// Verify
	got, err := store.Get("thread-001")
	require.NoError(t, err)
	assert.Equal(t, "Updated", got.Title)
	assert.Equal(t, "busy", got.Status)
	assert.GreaterOrEqual(t, got.UpdatedAt, meta.UpdatedAt)
}

func TestFileStore_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir)
	require.NoError(t, err)

	// Delete non-existent
	err = store.Delete("nonexistent")
	assert.Error(t, err)

	// Create and delete
	meta := &ThreadMetadata{ThreadID: "thread-001"}
	require.NoError(t, store.Create(meta))

	err = store.Delete("thread-001")
	require.NoError(t, err)

	// Verify deleted
	_, err = store.Get("thread-001")
	assert.Error(t, err)

	// Verify directory removed
	statePath := filepath.Join(tmpDir, "thread-001", "state.json")
	_, err = os.Stat(statePath)
	assert.True(t, os.IsNotExist(err))
}

func TestFileStore_Search(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir)
	require.NoError(t, err)

	// Create multiple threads
	for i := 1; i <= 5; i++ {
		status := "idle"
		if i%2 == 0 {
			status = "busy"
		}
		meta := &ThreadMetadata{
			ThreadID: fmt.Sprintf("thread-%03d", i),
			Status:   status,
		}
		require.NoError(t, store.Create(meta))
	}

	// Search all
	results, total, err := store.Search(SearchQuery{})
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, results, 5)

	// Search by status
	results, total, err = store.Search(SearchQuery{Status: "idle"})
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, results, 3)

	// Search with pagination
	results, total, err = store.Search(SearchQuery{Offset: 0, Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, results, 2)
}

func TestFileStore_SaveState(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir)
	require.NoError(t, err)

	// Create thread
	meta := &ThreadMetadata{ThreadID: "thread-001"}
	require.NoError(t, store.Create(meta))

	// Save state with messages
	state := &ThreadState{
		ThreadID: "thread-001",
		Title:    "Updated Title",
		Messages: []MessageRecord{
			{Role: "user", Content: "Hello", CreatedAt: time.Now().UnixMilli()},
			{Role: "assistant", Content: "Hi!", CreatedAt: time.Now().UnixMilli()},
		},
	}

	err = store.SaveState(state)
	require.NoError(t, err)

	// Verify
	got, err := store.GetState("thread-001")
	require.NoError(t, err)
	assert.Equal(t, "Updated Title", got.Title)
	assert.Len(t, got.Messages, 2)
	assert.Equal(t, "Hello", got.Messages[0].Content)
}

func TestFileStore_Persistence(t *testing.T) {
	tmpDir := t.TempDir()

	// Create store and add thread
	store1, err := NewFileStore(tmpDir)
	require.NoError(t, err)

	meta := &ThreadMetadata{ThreadID: "thread-001", Title: "Persistent"}
	require.NoError(t, store1.Create(meta))

	// Create new store instance (simulate restart)
	store2, err := NewFileStore(tmpDir)
	require.NoError(t, err)

	// Verify data persisted
	got, err := store2.Get("thread-001")
	require.NoError(t, err)
	assert.Equal(t, "Persistent", got.Title)
}

func TestFileStore_IndexFile(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir)
	require.NoError(t, err)

	// Create threads
	require.NoError(t, store.Create(&ThreadMetadata{ThreadID: "thread-001"}))
	require.NoError(t, store.Create(&ThreadMetadata{ThreadID: "thread-002"}))

	// Read index file directly
	indexPath := filepath.Join(tmpDir, "index.json")
	data, err := os.ReadFile(indexPath)
	require.NoError(t, err)

	var idx threadIndex
	require.NoError(t, json.Unmarshal(data, &idx))
	assert.Len(t, idx.Threads, 2)
}

func TestFileStore_List(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir)
	require.NoError(t, err)

	// List empty store
	threads, err := store.List()
	require.NoError(t, err)
	assert.Len(t, threads, 0)

	// Create threads
	for i := 1; i <= 3; i++ {
		meta := &ThreadMetadata{
			ThreadID: fmt.Sprintf("thread-%03d", i),
			Title:    fmt.Sprintf("Thread %d", i),
		}
		require.NoError(t, store.Create(meta))
	}

	// List all
	threads, err = store.List()
	require.NoError(t, err)
	assert.Len(t, threads, 3)
}

func TestFileStore_NewFileStore_DefaultDir(t *testing.T) {
	// Test with empty baseDir (uses default)
	store, err := NewFileStore("")
	require.NoError(t, err)
	assert.Equal(t, defaultThreadsDir, store.baseDir)

	// Cleanup
	_ = os.RemoveAll(defaultThreadsDir)
}

func TestFileStore_Update_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir)
	require.NoError(t, err)

	// Update non-existent thread
	meta := &ThreadMetadata{ThreadID: "nonexistent"}
	err = store.Update("nonexistent", meta)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestFileStore_GetState_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir)
	require.NoError(t, err)

	// Get state of non-existent thread
	_, err = store.GetState("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestFileStore_Create_WithMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir)
	require.NoError(t, err)

	// Create with custom metadata
	meta := &ThreadMetadata{
		ThreadID:  "thread-001",
		Title:     "Custom Title",
		Status:    "custom-status",
		CreatedAt: 1234567890,
		UpdatedAt: 1234567890,
		Metadata: map[string]any{
			"key1": "value1",
			"key2": 123,
		},
	}

	err = store.Create(meta)
	require.NoError(t, err)

	// Verify custom metadata preserved
	got, err := store.Get("thread-001")
	require.NoError(t, err)
	assert.Equal(t, "Custom Title", got.Title)
	assert.Equal(t, "custom-status", got.Status)
	assert.Equal(t, int64(1234567890), got.CreatedAt)
}

func TestFileStore_Search_Pagination(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir)
	require.NoError(t, err)

	// Create 10 threads
	for i := 1; i <= 10; i++ {
		meta := &ThreadMetadata{
			ThreadID: fmt.Sprintf("thread-%03d", i),
			Title:    fmt.Sprintf("Thread %d", i),
		}
		require.NoError(t, store.Create(meta))
	}

	// Test pagination with offset beyond results
	results, total, err := store.Search(SearchQuery{Offset: 100, Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, 10, total)
	assert.Len(t, results, 0)

	// Test pagination with negative offset (should be treated as 0)
	results, total, err = store.Search(SearchQuery{Offset: -1, Limit: 5})
	require.NoError(t, err)
	assert.Equal(t, 10, total)
	assert.Len(t, results, 5)

	// Test pagination with zero/negative limit (should return all)
	results, total, err = store.Search(SearchQuery{Offset: 0, Limit: 0})
	require.NoError(t, err)
	assert.Equal(t, 10, total)
	assert.Len(t, results, 10)

	results, total, err = store.Search(SearchQuery{Offset: 0, Limit: -1})
	require.NoError(t, err)
	assert.Equal(t, 10, total)
	assert.Len(t, results, 10)
}

func TestFileStore_Search_StatusFilter(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir)
	require.NoError(t, err)

	// Create threads with different statuses
	require.NoError(t, store.Create(&ThreadMetadata{ThreadID: "thread-001", Status: "idle"}))
	require.NoError(t, store.Create(&ThreadMetadata{ThreadID: "thread-002", Status: "busy"}))
	require.NoError(t, store.Create(&ThreadMetadata{ThreadID: "thread-003", Status: "idle"}))

	// Search for non-existent status
	results, total, err := store.Search(SearchQuery{Status: "nonexistent"})
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Len(t, results, 0)
}

func TestFileStore_Delete_Multiple(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileStore(tmpDir)
	require.NoError(t, err)

	// Create multiple threads
	for i := 1; i <= 5; i++ {
		meta := &ThreadMetadata{
			ThreadID: fmt.Sprintf("thread-%03d", i),
			Status:   "idle",
		}
		require.NoError(t, store.Create(meta))
	}

	// Delete one thread
	require.NoError(t, store.Delete("thread-003"))

	// Verify count
	results, total, err := store.Search(SearchQuery{})
	require.NoError(t, err)
	assert.Equal(t, 4, total)
	assert.Len(t, results, 4)

	// Verify deleted thread not found
	_, err = store.Get("thread-003")
	assert.Error(t, err)
}

func TestThreadIndex_List(t *testing.T) {
	idx := NewThreadIndex()

	// List empty index
	threads := idx.List()
	assert.Len(t, threads, 0)

	// Add threads
	for i := 1; i <= 5; i++ {
		meta := &ThreadMetadata{
			ThreadID: fmt.Sprintf("thread-%03d", i),
			Title:    fmt.Sprintf("Thread %d", i),
		}
		idx.Add(meta)
	}

	// List all
	threads = idx.List()
	assert.Len(t, threads, 5)
}

func TestThreadIndex_Count(t *testing.T) {
	idx := NewThreadIndex()

	// Count empty
	assert.Equal(t, 0, idx.Count())

	// Add threads
	for i := 1; i <= 3; i++ {
		meta := &ThreadMetadata{
			ThreadID: fmt.Sprintf("thread-%03d", i),
			Status:   "idle",
		}
		idx.Add(meta)
	}

	assert.Equal(t, 3, idx.Count())
}

func TestThreadIndex_CountByStatus(t *testing.T) {
	idx := NewThreadIndex()

	// Count by status with empty index
	assert.Equal(t, 0, idx.CountByStatus("idle"))

	// Add threads with different statuses
	idx.Add(&ThreadMetadata{ThreadID: "thread-001", Status: "idle"})
	idx.Add(&ThreadMetadata{ThreadID: "thread-002", Status: "busy"})
	idx.Add(&ThreadMetadata{ThreadID: "thread-003", Status: "idle"})

	assert.Equal(t, 2, idx.CountByStatus("idle"))
	assert.Equal(t, 1, idx.CountByStatus("busy"))
	assert.Equal(t, 0, idx.CountByStatus("nonexistent"))
}

func TestThreadIndex_Update_StatusChange(t *testing.T) {
	idx := NewThreadIndex()

	// Add thread
	meta := &ThreadMetadata{
		ThreadID: "thread-001",
		Status:   "idle",
	}
	idx.Add(meta)

	// Update with status change
	newMeta := &ThreadMetadata{
		ThreadID: "thread-001",
		Status:   "busy",
		Title:    "Updated",
	}
	updated := idx.Update("thread-001", newMeta)
	assert.True(t, updated)

	// Verify status index updated
	assert.Equal(t, 0, idx.CountByStatus("idle"))
	assert.Equal(t, 1, idx.CountByStatus("busy"))

	// Verify metadata updated
	got, exists := idx.Get("thread-001")
	assert.True(t, exists)
	assert.Equal(t, "busy", got.Status)
	assert.Equal(t, "Updated", got.Title)
}

func TestThreadIndex_Update_NonExistent(t *testing.T) {
	idx := NewThreadIndex()

	// Update non-existent thread
	meta := &ThreadMetadata{ThreadID: "nonexistent", Status: "idle"}
	updated := idx.Update("nonexistent", meta)
	assert.False(t, updated)
}

func TestThreadIndex_Delete_NonExistent(t *testing.T) {
	idx := NewThreadIndex()

	// Delete non-existent
	deleted := idx.Delete("nonexistent")
	assert.False(t, deleted)
}

func TestThreadIndex_Search_EdgeCases(t *testing.T) {
	idx := NewThreadIndex()

	// Add threads
	for i := 1; i <= 10; i++ {
		status := "idle"
		if i%2 == 0 {
			status = "busy"
		}
		idx.Add(&ThreadMetadata{
			ThreadID:  fmt.Sprintf("thread-%03d", i),
			Status:    status,
			CreatedAt: int64(i * 1000),
		})
	}

	// Search with large offset
	results, total := idx.Search(SearchQuery{Offset: 100, Limit: 10})
	assert.Equal(t, 10, total)
	assert.Len(t, results, 0)

	// Search with zero/negative limit
	results, total = idx.Search(SearchQuery{Limit: 0})
	assert.Equal(t, 10, total)
	assert.Len(t, results, 10)

	results, total = idx.Search(SearchQuery{Limit: -5})
	assert.Equal(t, 10, total)
	assert.Len(t, results, 10)

	// Search with negative offset
	results, total = idx.Search(SearchQuery{Offset: -10, Limit: 5})
	assert.Equal(t, 10, total)
	assert.Len(t, results, 5)
}

func TestThreadIndex_Delete_CleanupStatusIndex(t *testing.T) {
	idx := NewThreadIndex()

	// Add thread with unique status
	meta := &ThreadMetadata{ThreadID: "thread-001", Status: "unique-status"}
	idx.Add(meta)

	assert.Equal(t, 1, idx.CountByStatus("unique-status"))

	// Delete the thread
	deleted := idx.Delete("thread-001")
	assert.True(t, deleted)

	// Verify status index cleaned up
	assert.Equal(t, 0, idx.CountByStatus("unique-status"))
}
