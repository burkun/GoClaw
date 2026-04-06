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
	index   *threadIndex
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
	}

	// Ensure directory exists
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("create threads directory: %w", err)
	}

	// Load or create index
	if err := fs.loadIndex(); err != nil {
		return nil, fmt.Errorf("load index: %w", err)
	}

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

	// Check if already exists
	for _, t := range fs.index.Threads {
		if t.ThreadID == meta.ThreadID {
			return fmt.Errorf("thread %s already exists", meta.ThreadID)
		}
	}

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

	// Add to index
	fs.index.Threads = append(fs.index.Threads, meta)

	// Save index
	if err := fs.saveIndex(); err != nil {
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
func (fs *FileStore) Get(threadID string) (*ThreadMetadata, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	for _, t := range fs.index.Threads {
		if t.ThreadID == threadID {
			return t, nil
		}
	}

	return nil, fmt.Errorf("thread %s not found", threadID)
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
func (fs *FileStore) Update(threadID string, meta *ThreadMetadata) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Find and update in index
	found := false
	for i, t := range fs.index.Threads {
		if t.ThreadID == threadID {
			meta.ThreadID = threadID
			meta.CreatedAt = t.CreatedAt // Preserve creation time
			meta.UpdatedAt = time.Now().UnixMilli()
			fs.index.Threads[i] = meta
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("thread %s not found", threadID)
	}

	// Save index
	if err := fs.saveIndex(); err != nil {
		return fmt.Errorf("save index: %w", err)
	}

	return nil
}

// Delete removes a thread.
func (fs *FileStore) Delete(threadID string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Remove from index
	found := false
	newThreads := make([]*ThreadMetadata, 0, len(fs.index.Threads))
	for _, t := range fs.index.Threads {
		if t.ThreadID == threadID {
			found = true
			continue
		}
		newThreads = append(newThreads, t)
	}

	if !found {
		return fmt.Errorf("thread %s not found", threadID)
	}

	fs.index.Threads = newThreads

	// Save index
	if err := fs.saveIndex(); err != nil {
		return fmt.Errorf("save index: %w", err)
	}

	// Remove thread directory
	threadDir := fs.threadDir(threadID)
	return os.RemoveAll(threadDir)
}

// Search returns threads matching the query.
func (fs *FileStore) Search(query SearchQuery) ([]*ThreadMetadata, int, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	var results []*ThreadMetadata

	for _, t := range fs.index.Threads {
		// Filter by status
		if query.Status != "" && t.Status != query.Status {
			continue
		}

		results = append(results, t)
	}

	total := len(results)

	// Apply pagination
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > len(results) {
		offset = len(results)
	}

	limit := query.Limit
	if limit <= 0 {
		limit = len(results)
	}

	end := offset + limit
	if end > len(results) {
		end = len(results)
	}

	return results[offset:end], total, nil
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
func (fs *FileStore) List() ([]*ThreadMetadata, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	result := make([]*ThreadMetadata, len(fs.index.Threads))
	copy(result, fs.index.Threads)
	return result, nil
}
