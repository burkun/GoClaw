// Package threadstore provides thread persistence implementations.
// Thread storage saves conversation state to disk, enabling:
// - Cross-session conversation recovery
// - Historical thread listing and search
// - Checkpoint/resume functionality
package threadstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bookerbai/goclaw/pkg/metrics"
)

// ThreadMetadata represents the lightweight metadata stored in the index.
type ThreadMetadata struct {
	ThreadID  string         `json:"thread_id"`
	Title     string         `json:"title,omitempty"`
	Status    string         `json:"status"`
	CreatedAt int64          `json:"created_at"`
	UpdatedAt int64          `json:"updated_at"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// ThreadState represents the full thread state stored per-thread.
type ThreadState struct {
	ThreadID   string           `json:"thread_id"`
	Title      string           `json:"title,omitempty"`
	Status     string           `json:"status"`
	CreatedAt  int64            `json:"created_at"`
	UpdatedAt  int64            `json:"updated_at"`
	Metadata   map[string]any   `json:"metadata,omitempty"`
	Messages   []MessageRecord  `json:"messages,omitempty"`
	Checkpoint *CheckpointState `json:"checkpoint,omitempty"`
}

// MessageRecord represents a single message in the thread.
type MessageRecord struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at,omitempty"`
}

// CheckpointState represents an agent state checkpoint for resume.
type CheckpointState struct {
	CheckpointID string         `json:"checkpoint_id"`
	CreatedAt    int64          `json:"created_at"`
	State        map[string]any `json:"state,omitempty"`
}

// SearchQuery defines parameters for searching threads.
type SearchQuery struct {
	Status    string `json:"status,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
	Assistant string `json:"assistant,omitempty"`
}

// Store defines the interface for thread persistence.
type Store interface {
	// Create creates a new thread with the given metadata.
	Create(meta *ThreadMetadata) error

	// Get retrieves thread metadata by ID.
	Get(threadID string) (*ThreadMetadata, error)

	// GetState retrieves full thread state by ID.
	GetState(threadID string) (*ThreadState, error)

	// Update updates thread metadata.
	Update(threadID string, meta *ThreadMetadata) error

	// Delete removes a thread and its state.
	Delete(threadID string) error

	// Search returns threads matching the query.
	Search(query SearchQuery) ([]*ThreadMetadata, int, error)

	// SaveState persists full thread state.
	SaveState(state *ThreadState) error
}

// -----------------------------------------------------------------------------
// FileStore implementation
// -----------------------------------------------------------------------------

const (
	indexFileName     = "index.json"
	stateFileName     = "state.json"
	defaultThreadsDir = ".goclaw/threads"
)

// FileStore implements Store using local filesystem.
type FileStore struct {
	baseDir string
	mu      sync.RWMutex
	index   *threadIndex // Legacy index for JSON persistence
	idx     *ThreadIndex // High-performance in-memory index
}

type threadIndex struct {
	Threads []*ThreadMetadata `json:"threads"`
}

// NewFileStore creates a new file-based thread store.
func NewFileStore(baseDir string) (*FileStore, error) {
	if baseDir == "" {
		baseDir = defaultThreadsDir
	}

	fs := &FileStore{
		baseDir: baseDir,
		idx:     NewThreadIndex(),
	}

	// Ensure directory exists
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("create threads directory: %w", err)
	}

	// Load or create index
	if err := fs.loadIndex(); err != nil {
		return nil, fmt.Errorf("load index: %w", err)
	}

	// Rebuild high-performance index from loaded data
	fs.idx.Rebuild(fs.index.Threads)

	return fs, nil
}

func (fs *FileStore) loadIndex() error {
	indexPath := filepath.Join(fs.baseDir, indexFileName)

	data, err := os.ReadFile(indexPath)
	if os.IsNotExist(err) {
		fs.index = &threadIndex{Threads: []*ThreadMetadata{}}
		return fs.saveIndex()
	}
	if err != nil {
		return err
	}

	var idx threadIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return fmt.Errorf("unmarshal index: %w", err)
	}

	fs.index = &idx
	return nil
}

func (fs *FileStore) saveIndex() error {
	indexPath := filepath.Join(fs.baseDir, indexFileName)

	data, err := json.MarshalIndent(fs.index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}

	return os.WriteFile(indexPath, data, 0644)
}

func (fs *FileStore) threadDir(threadID string) string {
	return filepath.Join(fs.baseDir, threadID)
}

func (fs *FileStore) statePath(threadID string) string {
	return filepath.Join(fs.threadDir(threadID), stateFileName)
}

// Create creates a new thread.
func (fs *FileStore) Create(meta *ThreadMetadata) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Check if already exists using O(1) lookup
	if _, exists := fs.idx.Get(meta.ThreadID); exists {
		return fmt.Errorf("thread %s already exists", meta.ThreadID)
	}

	// Update active threads metric.
	defer func() {
		metrics.SetActiveThreads(float64(len(fs.index.Threads)))
	}()

	// Create thread directory
	threadDir := fs.threadDir(meta.ThreadID)
	if err := os.MkdirAll(threadDir, 0755); err != nil {
		return fmt.Errorf("create thread directory: %w", err)
	}

	// Initialize timestamps
	now := time.Now().UnixMilli()
	if meta.CreatedAt == 0 {
		meta.CreatedAt = now
	}
	if meta.UpdatedAt == 0 {
		meta.UpdatedAt = now
	}
	if meta.Status == "" {
		meta.Status = "idle"
	}

	// Add to legacy index (for persistence)
	fs.index.Threads = append(fs.index.Threads, meta)

	// Add to high-performance index (for queries)
	fs.idx.Add(meta)

	// Save index
	if err := fs.saveIndex(); err != nil {
		// Rollback: remove from index on error.
		fs.index.Threads = fs.index.Threads[:len(fs.index.Threads)-1]
		return fmt.Errorf("save index: %w", err)
	}

	// Save initial state
	state := &ThreadState{
		ThreadID:  meta.ThreadID,
		Title:     meta.Title,
		Status:    meta.Status,
		CreatedAt: meta.CreatedAt,
		UpdatedAt: meta.UpdatedAt,
		Metadata:  meta.Metadata,
		Messages:  make([]MessageRecord, 0),
	}

	return fs.saveStateLocked(state)
}

// Get retrieves thread metadata by ID.
// Optimized: O(1) lookup using hash index.
func (fs *FileStore) Get(threadID string) (*ThreadMetadata, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	meta, exists := fs.idx.Get(threadID)
	if !exists {
		return nil, fmt.Errorf("thread %s not found", threadID)
	}
	return meta, nil
}

// GetState retrieves full thread state.
func (fs *FileStore) GetState(threadID string) (*ThreadState, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	statePath := fs.statePath(threadID)
	data, err := os.ReadFile(statePath)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("thread %s not found", threadID)
	}
	if err != nil {
		return nil, fmt.Errorf("read state file: %w", err)
	}

	var state ThreadState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}

	return &state, nil
}

// Update updates thread metadata.
// Optimized: O(1) lookup using hash index.
func (fs *FileStore) Update(threadID string, meta *ThreadMetadata) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Check existence using O(1) lookup
	oldMeta, exists := fs.idx.Get(threadID)
	if !exists {
		return fmt.Errorf("thread %s not found", threadID)
	}

	// Preserve creation time
	meta.ThreadID = threadID
	meta.CreatedAt = oldMeta.CreatedAt
	meta.UpdatedAt = time.Now().UnixMilli()

	// Update legacy index
	found := false
	for i, t := range fs.index.Threads {
		if t.ThreadID == threadID {
			fs.index.Threads[i] = meta
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("thread %s not found in legacy index", threadID)
	}

	// Update high-performance index
	fs.idx.Update(threadID, meta)

	// Save index
	if err := fs.saveIndex(); err != nil {
		return fmt.Errorf("save index: %w", err)
	}

	return nil
}

// Delete removes a thread.
// Optimized: O(1) existence check using hash index.
func (fs *FileStore) Delete(threadID string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Check existence using O(1) lookup
	if _, exists := fs.idx.Get(threadID); !exists {
		return fmt.Errorf("thread %s not found", threadID)
	}

	// Remove from legacy index
	newThreads := make([]*ThreadMetadata, 0, len(fs.index.Threads))
	for _, t := range fs.index.Threads {
		if t.ThreadID != threadID {
			newThreads = append(newThreads, t)
		}
	}
	fs.index.Threads = newThreads

	// Remove from high-performance index
	fs.idx.Delete(threadID)

	// Update active threads metric.
	defer func() {
		metrics.SetActiveThreads(float64(len(fs.index.Threads)))
	}()

	// Save index
	if err := fs.saveIndex(); err != nil {
		return fmt.Errorf("save index: %w", err)
	}

	// Remove thread directory
	threadDir := fs.threadDir(threadID)
	return os.RemoveAll(threadDir)
}

// Search returns threads matching the query.
// Optimized: Uses indexed search for O(k) performance where k is result size.
func (fs *FileStore) Search(query SearchQuery) ([]*ThreadMetadata, int, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	// Use high-performance indexed search
	results, total := fs.idx.Search(query)
	return results, total, nil
}

// SaveState persists full thread state.
func (fs *FileStore) SaveState(state *ThreadState) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	return fs.saveStateLocked(state)
}

func (fs *FileStore) saveStateLocked(state *ThreadState) error {
	threadDir := fs.threadDir(state.ThreadID)

	// Ensure thread directory exists
	if err := os.MkdirAll(threadDir, 0755); err != nil {
		return fmt.Errorf("create thread directory: %w", err)
	}

	state.UpdatedAt = time.Now().UnixMilli()

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	statePath := fs.statePath(state.ThreadID)
	return os.WriteFile(statePath, data, 0644)
}

// List returns all threads (convenience method).
// Optimized: Returns pre-sorted list from index.
func (fs *FileStore) List() ([]*ThreadMetadata, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	return fs.idx.List(), nil
}
