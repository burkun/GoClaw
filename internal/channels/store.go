// Package channels provides channel storage for IM integrations.
//
// ChannelStore persists IM chat-to-GoClaw thread mappings, supporting
// topic-level isolation for Feishu threads, Slack threads, and Telegram topics.
//
// This mirrors DeerFlow's ChannelStore implementation.
package channels

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// StoreEntry represents a single mapping entry.
type StoreEntry struct {
	ThreadID  string    `json:"thread_id"`
	UserID    string    `json:"user_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ChannelStore persists IM chat-to-GoClaw thread mappings.
//
// Data layout (on disk):
//
//	{
//	  "<channel_name>:<chat_id>": {
//	    "thread_id": "<uuid>",
//	    "user_id": "<platform_user>",
//	    "created_at": "2024-01-01T00:00:00Z",
//	    "updated_at": "2024-01-01T00:00:00Z"
//	  },
//	  "<channel_name>:<chat_id>:<topic_id>": { ... },
//	  ...
//	}
//
// The store supports topic-level isolation via the optional topic_id parameter.
type ChannelStore interface {
	// GetThreadID looks up the GoClaw thread_id for a given IM conversation/topic.
	GetThreadID(channelName, chatID string, topicID *string) (string, bool)

	// SetThreadID creates or updates the mapping for an IM conversation/topic.
	SetThreadID(channelName, chatID, threadID string, topicID *string, userID string)

	// Remove deletes a mapping. If topicID is nil, removes all mappings for the channel/chat.
	Remove(channelName, chatID string, topicID *string) bool

	// ListEntries lists all stored mappings, optionally filtered by channel.
	ListEntries(channelName *string) []StoreEntryWithKey
}

// StoreEntryWithKey is a StoreEntry with its key information.
type StoreEntryWithKey struct {
	ChannelName string `json:"channel_name"`
	ChatID      string `json:"chat_id"`
	TopicID     string `json:"topic_id,omitempty"`
	StoreEntry
}

// FileChannelStore is a JSON-file-backed store.
// This mirrors DeerFlow's ChannelStore implementation.
type FileChannelStore struct {
	mu   sync.RWMutex
	path string
	data map[string]StoreEntry
}

// NewFileChannelStore creates a new file-backed channel store.
// If path is empty, uses a default location.
func NewFileChannelStore(path string) (*FileChannelStore, error) {
	if path == "" {
		// Use default path: ~/.goclaw/channels/store.json
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("get home directory: %w", err)
		}
		path = filepath.Join(home, ".goclaw", "channels", "store.json")
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create store directory: %w", err)
	}

	s := &FileChannelStore{
		path: path,
		data: make(map[string]StoreEntry),
	}

	// Load existing data
	if err := s.load(); err != nil {
		// Log warning but continue with empty store
		_ = err
	}

	return s, nil
}

// NewInMemoryChannelStore creates an in-memory-only store (for testing).
func NewInMemoryChannelStore() *FileChannelStore {
	return &FileChannelStore{
		path: "", // No persistence
		data: make(map[string]StoreEntry),
	}
}

// load reads the store from disk.
func (s *FileChannelStore) load() error {
	if s.path == "" {
		return nil // In-memory store
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // New store
		}
		return fmt.Errorf("read store file: %w", err)
	}

	if len(data) == 0 {
		return nil // Empty file
	}

	if err := json.Unmarshal(data, &s.data); err != nil {
		return fmt.Errorf("parse store file: %w", err)
	}

	return nil
}

// save writes the store to disk atomically.
func (s *FileChannelStore) save() error {
	if s.path == "" {
		return nil // In-memory store
	}

	// Marshal with indentation for readability
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal store data: %w", err)
	}

	// Write to temp file then rename for atomicity
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write temp store file: %w", err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		os.Remove(tmpPath) // Clean up on error
		return fmt.Errorf("rename store file: %w", err)
	}

	return nil
}

// makeKey creates a storage key from components.
// Mirrors DeerFlow's ChannelStore._key method.
func makeKey(channelName, chatID string, topicID *string) string {
	if topicID != nil && *topicID != "" {
		return fmt.Sprintf("%s:%s:%s", channelName, chatID, *topicID)
	}
	return fmt.Sprintf("%s:%s", channelName, chatID)
}

// GetThreadID looks up the GoClaw thread_id for a given IM conversation/topic.
func (s *FileChannelStore) GetThreadID(channelName, chatID string, topicID *string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := makeKey(channelName, chatID, topicID)
	entry, ok := s.data[key]
	if !ok {
		return "", false
	}
	return entry.ThreadID, true
}

// SetThreadID creates or updates the mapping for an IM conversation/topic.
func (s *FileChannelStore) SetThreadID(channelName, chatID, threadID string, topicID *string, userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := makeKey(channelName, chatID, topicID)
	now := time.Now().UTC()

	existing, hasExisting := s.data[key]
	entry := StoreEntry{
		ThreadID:  threadID,
		UserID:    userID,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if hasExisting {
		entry.CreatedAt = existing.CreatedAt
	}

	s.data[key] = entry

	// Persist to disk
	_ = s.save()
}

// Remove deletes a mapping.
// If topicID is nil, removes all mappings for the channel/chat (including topic-specific ones).
// Returns true if at least one mapping was removed.
func (s *FileChannelStore) Remove(channelName, chatID string, topicID *string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	removed := false

	if topicID != nil {
		// Remove specific topic mapping
		key := makeKey(channelName, chatID, topicID)
		if _, ok := s.data[key]; ok {
			delete(s.data, key)
			removed = true
		}
	} else {
		// Remove all mappings for this channel/chat
		baseKey := makeKey(channelName, chatID, nil)
		prefix := baseKey + ":"

		for key := range s.data {
			if key == baseKey || strings.HasPrefix(key, prefix) {
				delete(s.data, key)
				removed = true
			}
		}
	}

	if removed {
		_ = s.save()
	}

	return removed
}

// ListEntries lists all stored mappings, optionally filtered by channel.
func (s *FileChannelStore) ListEntries(channelName *string) []StoreEntryWithKey {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]StoreEntryWithKey, 0, len(s.data))

	for key, entry := range s.data {
		parts := strings.SplitN(key, ":", 3)
		if len(parts) < 2 {
			continue
		}

		ch := parts[0]
		chat := parts[1]

		// Filter by channel if specified
		if channelName != nil && ch != *channelName {
			continue
		}

		item := StoreEntryWithKey{
			ChannelName: ch,
			ChatID:      chat,
			StoreEntry:  entry,
		}

		// Topic ID is the third part if present
		if len(parts) > 2 {
			item.TopicID = parts[2]
		}

		results = append(results, item)
	}

	return results
}

// Ensure FileChannelStore satisfies ChannelStore at compile time.
var _ ChannelStore = (*FileChannelStore)(nil)

// Legacy InMemoryChannelStore for backward compatibility.
// Deprecated: Use FileChannelStore instead.
type InMemoryChannelStore struct {
	mu   sync.RWMutex
	data map[string]string
}

// NewInMemoryChannelStore creates an empty mapping store (legacy).
// Deprecated: Use NewFileChannelStore or NewInMemoryChannelStore from FileChannelStore.
func NewLegacyInMemoryChannelStore() *InMemoryChannelStore {
	return &InMemoryChannelStore{data: map[string]string{}}
}

func legacyStoreKey(channel, chatID string) string {
	return channel + "::" + chatID
}

func (s *InMemoryChannelStore) GetThreadID(channel, chatID string, topicID *string) (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[legacyStoreKey(channel, chatID)]
	return v, ok
}

func (s *InMemoryChannelStore) SetThreadID(channel, chatID, threadID string, topicID *string, userID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.data[legacyStoreKey(channel, chatID)] = threadID
	s.mu.Unlock()
}

func (s *InMemoryChannelStore) Remove(channel, chatID string, topicID *string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := legacyStoreKey(channel, chatID)
	if _, ok := s.data[key]; ok {
		delete(s.data, key)
		return true
	}
	return false
}

func (s *InMemoryChannelStore) ListEntries(channelName *string) []StoreEntryWithKey {
	return nil // Not implemented for legacy store
}

var _ ChannelStore = (*InMemoryChannelStore)(nil)
