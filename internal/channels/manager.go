package channels

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ThreadIDGenerator decides thread_id for a new (channel,chat_id) pair.
type ThreadIDGenerator func(msg IncomingMessage) string

// Manager orchestrates channels, mapping store, and processor dispatch.
type Manager struct {
	mu sync.RWMutex

	channels map[string]Channel
	bus      *MessageBus
	store    ChannelStore
	proc     Processor
	gen      ThreadIDGenerator
}

// NewManager creates a channels manager.
func NewManager(store ChannelStore, proc Processor) *Manager {
	if store == nil {
		store = NewInMemoryChannelStore()
	}
	return &Manager{
		channels: map[string]Channel{},
		bus:      NewMessageBus(),
		store:    store,
		proc:     proc,
		gen:      defaultThreadIDGenerator,
	}
}

func defaultThreadIDGenerator(msg IncomingMessage) string {
	return fmt.Sprintf("%s-%s-%d", msg.Channel, msg.ChatID, time.Now().UnixNano())
}

// SetThreadIDGenerator overrides the default thread id generation strategy.
func (m *Manager) SetThreadIDGenerator(gen ThreadIDGenerator) {
	if gen == nil {
		return
	}
	m.mu.Lock()
	m.gen = gen
	m.mu.Unlock()
}

// RegisterChannel adds one adapter into manager.
func (m *Manager) RegisterChannel(ch Channel) error {
	if ch == nil {
		return fmt.Errorf("channel is nil")
	}
	name := ch.Name()
	if name == "" {
		return fmt.Errorf("channel name is empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.channels[name]; exists {
		return fmt.Errorf("channel %q already registered", name)
	}
	m.channels[name] = ch
	return nil
}

// Start starts all registered channels.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.RLock()
	chs := make([]Channel, 0, len(m.channels))
	for _, ch := range m.channels {
		chs = append(chs, ch)
	}
	m.mu.RUnlock()

	for _, ch := range chs {
		if err := ch.Start(ctx, m.onIncoming); err != nil {
			return fmt.Errorf("start channel %s: %w", ch.Name(), err)
		}
	}
	return nil
}

// Stop stops all registered channels.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.RLock()
	chs := make([]Channel, 0, len(m.channels))
	for _, ch := range m.channels {
		chs = append(chs, ch)
	}
	m.mu.RUnlock()

	for _, ch := range chs {
		if err := ch.Stop(ctx); err != nil {
			return fmt.Errorf("stop channel %s: %w", ch.Name(), err)
		}
	}
	return nil
}

// Bus returns the event bus for external subscription.
func (m *Manager) Bus() *MessageBus { return m.bus }

func (m *Manager) onIncoming(ctx context.Context, msg IncomingMessage) error {
	if msg.Channel == "" || msg.ChatID == "" {
		return fmt.Errorf("invalid incoming message: channel/chat_id required")
	}

	threadID, ok := m.store.GetThreadID(msg.Channel, msg.ChatID)
	if !ok || threadID == "" {
		m.mu.RLock()
		gen := m.gen
		m.mu.RUnlock()
		threadID = gen(msg)
		m.store.SetThreadID(msg.Channel, msg.ChatID, threadID)
	}
	msg.ThreadID = threadID
	m.bus.Publish(msg)

	m.mu.RLock()
	proc := m.proc
	ch := m.channels[msg.Channel]
	m.mu.RUnlock()
	if proc == nil || ch == nil {
		return nil
	}

	out, err := proc.Process(ctx, msg, threadID)
	if err != nil {
		return err
	}
	if out.Channel == "" {
		out.Channel = msg.Channel
	}
	if out.ChatID == "" {
		out.ChatID = msg.ChatID
	}
	out.ThreadID = threadID
	return ch.Send(ctx, out)
}
