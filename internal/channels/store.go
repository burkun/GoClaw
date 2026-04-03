package channels

import "sync"

// ChannelStore keeps chat_id -> thread_id mappings per channel.
type ChannelStore interface {
	GetThreadID(channel, chatID string) (string, bool)
	SetThreadID(channel, chatID, threadID string)
	DeleteThreadID(channel, chatID string)
}

// InMemoryChannelStore is the default in-memory implementation.
type InMemoryChannelStore struct {
	mu   sync.RWMutex
	data map[string]string
}

// NewInMemoryChannelStore creates an empty mapping store.
func NewInMemoryChannelStore() *InMemoryChannelStore {
	return &InMemoryChannelStore{data: map[string]string{}}
}

func storeKey(channel, chatID string) string {
	return channel + "::" + chatID
}

func (s *InMemoryChannelStore) GetThreadID(channel, chatID string) (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[storeKey(channel, chatID)]
	return v, ok
}

func (s *InMemoryChannelStore) SetThreadID(channel, chatID, threadID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.data[storeKey(channel, chatID)] = threadID
	s.mu.Unlock()
}

func (s *InMemoryChannelStore) DeleteThreadID(channel, chatID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.data, storeKey(channel, chatID))
	s.mu.Unlock()
}

var _ ChannelStore = (*InMemoryChannelStore)(nil)
