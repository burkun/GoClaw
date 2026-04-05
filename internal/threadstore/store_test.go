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
