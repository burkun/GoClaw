package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"goclaw/pkg/errors"
)

// ---------------------------------------------------------------------------
// Data structures
// ---------------------------------------------------------------------------

// ContextSection holds one summary section with update timestamp.
type ContextSection struct {
	Summary   string `json:"summary"`
	UpdatedAt string `json:"updatedAt"`
}

// UserContext groups user-related context sections.
type UserContext struct {
	WorkContext     ContextSection `json:"workContext"`
	PersonalContext ContextSection `json:"personalContext"`
	TopOfMind       ContextSection `json:"topOfMind"`
}

// HistoryContext groups history-related context sections.
type HistoryContext struct {
	RecentMonths       ContextSection `json:"recentMonths"`
	EarlierContext     ContextSection `json:"earlierContext"`
	LongTermBackground ContextSection `json:"longTermBackground"`
}

// MemoryFact is the persisted structured fact schema.
type MemoryFact struct {
	ID          string  `json:"id"`
	Content     string  `json:"content"`
	Category    string  `json:"category"`
	Confidence  float64 `json:"confidence"`
	CreatedAt   string  `json:"createdAt"`
	Source      string  `json:"source"`
	SourceError *string `json:"sourceError,omitempty"`
}

// Memory is the persisted memory document for a single agent scope.
// It matches the frontend/gateway memory schema.
type Memory struct {
	Version     string         `json:"version"`
	LastUpdated string         `json:"lastUpdated"`
	User        UserContext    `json:"user"`
	History     HistoryContext `json:"history"`
	Facts       []MemoryFact   `json:"facts"`
}

// newEmptyMemory returns an initialised Memory document with current timestamp.
func newEmptyMemory() *Memory {
	return &Memory{
		Version:     "1.0",
		LastUpdated: time.Now().UTC().Format(time.RFC3339),
		Facts:       []MemoryFact{},
	}
}

func cloneMemory(m *Memory) *Memory {
	if m == nil {
		return nil
	}
	out := *m
	out.Facts = append([]MemoryFact(nil), m.Facts...)
	return &out
}

// ---------------------------------------------------------------------------
// MemoryStore interface
// ---------------------------------------------------------------------------

// MemoryStore is the persistence abstraction used by MemoryMiddleware.
//
// Implementations must be safe for concurrent use — Load and Save can be
// called from multiple goroutines (the update queue worker + the agent goroutine).
type MemoryStore interface {
	// Load returns the current Memory document. Implementations should cache
	// with mtime-based invalidation to avoid redundant disk reads.
	Load() (*Memory, error)

	// Save atomically persists the Memory document.
	// Implementations must update LastUpdated before writing.
	Save(m *Memory) error

	// AddFact appends a new fact if it is not already present
	// (case-insensitive substring deduplication on content). Returns true if
	// the fact was added, false if it was considered a duplicate.
	AddFact(m *Memory, fact MemoryFact) bool

	// Deduplicate removes semantically duplicate facts from m.Facts in place.
	// The default implementation uses case-insensitive substring matching;
	// callers may replace this with an LLM-based deduplication pass.
	Deduplicate(m *Memory)
}

// ---------------------------------------------------------------------------
// JSONFileStore — default MemoryStore implementation
// ---------------------------------------------------------------------------

// JSONFileStore stores Memory as a JSON file with atomic rename-on-write.
//
// Atomic writes prevent partial reads: the document is written to a *.tmp
// sibling and then renamed into place, matching DeerFlow's FileMemoryStorage.
type JSONFileStore struct {
	path string

	mu       sync.RWMutex
	cache    *Memory
	cacheMod time.Time // mtime of the file when cache was populated
}

// NewJSONFileStore creates a JSONFileStore backed by the given file path.
// The directory must already exist; NewJSONFileStore does not create it.
func NewJSONFileStore(path string) *JSONFileStore {
	return &JSONFileStore{path: path}
}

// Load returns the Memory document, using a cached copy when the file has not
// changed since the last read (mtime comparison).
func (s *JSONFileStore) Load() (*Memory, error) {
	info, statErr := os.Stat(s.path)
	if statErr != nil && !os.IsNotExist(statErr) {
		return nil, errors.WrapInternalError(statErr, fmt.Sprintf("memory: stat %s", s.path))
	}

	s.mu.RLock()
	if s.cache != nil {
		if statErr == nil && info.ModTime().Equal(s.cacheMod) {
			m := cloneMemory(s.cache)
			s.mu.RUnlock()
			return m, nil
		}
		if os.IsNotExist(statErr) && s.cacheMod.IsZero() {
			m := cloneMemory(s.cache)
			s.mu.RUnlock()
			return m, nil
		}
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	info, statErr = os.Stat(s.path)
	if os.IsNotExist(statErr) {
		m := newEmptyMemory()
		s.cache = cloneMemory(m)
		s.cacheMod = time.Time{}
		return m, nil
	}
	if statErr != nil {
		return nil, errors.WrapInternalError(statErr, fmt.Sprintf("memory: stat %s", s.path))
	}

	if s.cache != nil && info.ModTime().Equal(s.cacheMod) {
		return cloneMemory(s.cache), nil
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, errors.WrapInternalError(err, fmt.Sprintf("memory: read %s", s.path))
	}

	m := newEmptyMemory()
	if err := json.Unmarshal(data, m); err != nil {
		return nil, errors.WrapInternalError(err, fmt.Sprintf("memory: unmarshal %s", s.path))
	}

	s.cache = cloneMemory(m)
	s.cacheMod = info.ModTime()
	return m, nil
}

// Save atomically writes m to the backing file.
// It updates LastUpdated before writing and refreshes the in-memory cache.
func (s *JSONFileStore) Save(m *Memory) error {
	m.LastUpdated = time.Now().UTC().Format(time.RFC3339)

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return errors.WrapInternalError(err, "memory: marshal")
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return errors.WrapInternalError(err, fmt.Sprintf("memory: write tmp %s", tmp))
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return errors.WrapInternalError(err, fmt.Sprintf("memory: rename %s → %s", tmp, s.path))
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = cloneMemory(m)
	if info, err := os.Stat(s.path); err == nil {
		s.cacheMod = info.ModTime()
	}
	return nil
}

// AddFact appends fact to m.Facts if no existing fact contains the same
// content as a case-insensitive substring. Returns true if fact was added.
func (s *JSONFileStore) AddFact(m *Memory, fact MemoryFact) bool {
	fact.Content = strings.TrimSpace(fact.Content)
	if fact.Content == "" {
		return false
	}
	lf := strings.ToLower(fact.Content)
	for _, f := range m.Facts {
		if strings.Contains(strings.ToLower(strings.TrimSpace(f.Content)), lf) {
			return false
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(fact.ID) == "" {
		fact.ID = newFactID()
	}
	if strings.TrimSpace(fact.Category) == "" {
		fact.Category = "context"
	}
	if fact.Confidence <= 0 {
		fact.Confidence = 0.8
	}
	if strings.TrimSpace(fact.CreatedAt) == "" {
		fact.CreatedAt = now
	}
	if strings.TrimSpace(fact.Source) == "" {
		fact.Source = "unknown"
	}

	m.Facts = append(m.Facts, fact)
	return true
}

// Deduplicate removes redundant facts from m.Facts.
// If one fact's content fully contains another fact's content (case-insensitive),
// the shorter one is considered dominated and removed.
func (s *JSONFileStore) Deduplicate(m *Memory) {
	if len(m.Facts) <= 1 {
		return
	}

	dominated := make([]bool, len(m.Facts))
	for i := range m.Facts {
		li := strings.ToLower(strings.TrimSpace(m.Facts[i].Content))
		if li == "" {
			dominated[i] = true
			continue
		}
		for j := range m.Facts {
			if i == j || dominated[j] {
				continue
			}
			lj := strings.ToLower(strings.TrimSpace(m.Facts[j].Content))
			if lj == "" {
				dominated[j] = true
				continue
			}
			if strings.Contains(li, lj) && li != lj {
				dominated[j] = true
			}
		}
	}

	kept := m.Facts[:0]
	for i, f := range m.Facts {
		if !dominated[i] {
			kept = append(kept, f)
		}
	}
	m.Facts = kept
}
