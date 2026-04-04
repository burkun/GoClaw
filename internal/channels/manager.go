package channels

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ThreadIDGenerator decides thread_id for a new (channel,chat_id) pair.
type ThreadIDGenerator func(msg IncomingMessage) string

// Manager orchestrates channels, mapping store, and processor dispatch.
type Manager struct {
	mu sync.RWMutex

	channels       map[string]Channel
	bus            *MessageBus
	store          ChannelStore
	proc           Processor
	gen            ThreadIDGenerator
	running        bool
	maxConcurrency int
	sem            chan struct{}
}

// NewManager creates a channels manager.
func NewManager(store ChannelStore, proc Processor) *Manager {
	if store == nil {
		store = NewInMemoryChannelStore()
	}
	maxConc := 10
	return &Manager{
		channels:       map[string]Channel{},
		bus:            NewMessageBus(),
		store:          store,
		proc:           proc,
		gen:            defaultThreadIDGenerator,
		maxConcurrency: maxConc,
		sem:            make(chan struct{}, maxConc),
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
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil
	}
	m.running = true
	chs := make([]Channel, 0, len(m.channels))
	for _, ch := range m.channels {
		chs = append(chs, ch)
	}
	m.mu.Unlock()

	for _, ch := range chs {
		if err := ch.Start(ctx, m.onIncoming); err != nil {
			return fmt.Errorf("start channel %s: %w", ch.Name(), err)
		}
	}
	return nil
}

// Stop stops all registered channels.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return nil
	}
	m.running = false
	chs := make([]Channel, 0, len(m.channels))
	for _, ch := range m.channels {
		chs = append(chs, ch)
	}
	m.mu.Unlock()

	for _, ch := range chs {
		if err := ch.Stop(ctx); err != nil {
			return fmt.Errorf("stop channel %s: %w", ch.Name(), err)
		}
	}
	return nil
}

// IsRunning returns whether the manager is running.
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// GetChannelStatus returns status info for all channels.
func (m *Manager) GetChannelStatus() map[string]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	status := make(map[string]bool, len(m.channels))
	for name := range m.channels {
		status[name] = m.running
	}
	return status
}

// RestartChannel restarts a specific channel.
func (m *Manager) RestartChannel(ctx context.Context, name string) error {
	m.mu.RLock()
	ch, exists := m.channels[name]
	m.mu.RUnlock()
	if !exists {
		return fmt.Errorf("channel %q not found", name)
	}
	if err := ch.Stop(ctx); err != nil {
		return fmt.Errorf("stop channel %s: %w", name, err)
	}
	if err := ch.Start(ctx, m.onIncoming); err != nil {
		return fmt.Errorf("start channel %s: %w", name, err)
	}
	return nil
}

// Bus returns the event bus for external subscription.
func (m *Manager) Bus() *MessageBus { return m.bus }

func (m *Manager) onIncoming(ctx context.Context, msg IncomingMessage) error {
	if msg.Channel == "" || msg.ChatID == "" {
		return fmt.Errorf("invalid incoming message: channel/chat_id required")
	}

	// Check for commands.
	if strings.HasPrefix(strings.TrimSpace(msg.Text), "/") {
		return m.handleCommand(ctx, msg)
	}

	// Acquire semaphore for concurrency control.
	select {
	case m.sem <- struct{}{}:
		defer func() { <-m.sem }()
	case <-ctx.Done():
		return ctx.Err()
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

func (m *Manager) handleCommand(ctx context.Context, msg IncomingMessage) error {
	text := strings.TrimSpace(msg.Text)
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return nil
	}

	cmd := strings.ToLower(parts[0])
	m.mu.RLock()
	ch := m.channels[msg.Channel]
	m.mu.RUnlock()

	var response string
	switch cmd {
	case "/new":
		// Clear thread mapping to start fresh.
		m.store.DeleteThreadID(msg.Channel, msg.ChatID)
		response = "Started a new conversation."
	case "/status":
		m.mu.RLock()
		running := m.running
		channelCount := len(m.channels)
		m.mu.RUnlock()
		response = fmt.Sprintf("Service running: %v, Channels: %d", running, channelCount)
	case "/help":
		response = "Available commands:\n/new - Start a new conversation\n/status - Show service status\n/help - Show this help"
	default:
		response = fmt.Sprintf("Unknown command: %s. Use /help for available commands.", cmd)
	}

	if ch != nil && response != "" {
		out := OutgoingMessage{
			Channel: msg.Channel,
			ChatID:  msg.ChatID,
			Text:    response,
		}
		return ch.Send(ctx, out)
	}
	return nil
}
