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
	channelCaps    map[string]bool
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
		channelCaps: map[string]bool{
			"feishu":   true,
			"slack":    false,
			"telegram": false,
		},
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

// SetChannelStreamingCapability sets whether a channel supports streaming dispatch.
func (m *Manager) SetChannelStreamingCapability(channel string, supports bool) {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		return
	}
	m.mu.Lock()
	if m.channelCaps == nil {
		m.channelCaps = make(map[string]bool)
	}
	m.channelCaps[channel] = supports
	m.mu.Unlock()
}

func (m *Manager) channelSupportsStreaming(channel string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.channelCaps == nil {
		return false
	}
	supported, ok := m.channelCaps[channel]
	return ok && supported
}

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

	var topicIDPtr *string
	if msg.TopicID != "" {
		topicIDPtr = &msg.TopicID
	}
	threadID, ok := m.store.GetThreadID(msg.Channel, msg.ChatID, topicIDPtr)
	if !ok || threadID == "" {
		m.mu.RLock()
		gen := m.gen
		m.mu.RUnlock()
		threadID = gen(msg)
		m.store.SetThreadID(msg.Channel, msg.ChatID, threadID, topicIDPtr, msg.UserID)
	}
	msg.ThreadID = threadID
	m.bus.Publish(msg)

	m.mu.RLock()
	proc := m.proc
	m.mu.RUnlock()
	if proc == nil {
		return nil
	}

	if streamProc, ok := proc.(StreamProcessor); ok && m.channelSupportsStreaming(msg.Channel) {
		return m.handleStreamingIncoming(ctx, msg, threadID, streamProc)
	}

	out, err := proc.Process(ctx, msg, threadID)
	if err != nil {
		return err
	}
	m.enrichOutgoing(&out, msg, threadID)
	out.IsFinal = true
	return m.dispatchOutgoing(ctx, out)
}

func (m *Manager) handleStreamingIncoming(ctx context.Context, msg IncomingMessage, threadID string, proc StreamProcessor) error {
	stream, err := proc.ProcessStream(ctx, msg, threadID)
	if err != nil {
		return err
	}
	if stream == nil {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out, ok := <-stream:
			if !ok {
				return nil
			}
			m.enrichOutgoing(&out, msg, threadID)
			if err := m.dispatchOutgoing(ctx, out); err != nil {
				return err
			}
		}
	}
}

func (m *Manager) enrichOutgoing(out *OutgoingMessage, incoming IncomingMessage, threadID string) {
	if out == nil {
		return
	}
	if out.Channel == "" {
		out.Channel = incoming.Channel
	}
	if out.ChatID == "" {
		out.ChatID = incoming.ChatID
	}
	if out.TopicID == "" {
		out.TopicID = incoming.TopicID
	}
	out.ThreadID = threadID

	if out.Metadata == nil {
		out.Metadata = map[string]any{}
	}
	for k, v := range incoming.Metadata {
		if _, exists := out.Metadata[k]; !exists {
			out.Metadata[k] = v
		}
	}
	if incoming.MessageID != "" {
		if _, exists := out.Metadata["source_message_id"]; !exists {
			out.Metadata["source_message_id"] = incoming.MessageID
		}
		if _, exists := out.Metadata["thread_ts"]; !exists {
			out.Metadata["thread_ts"] = incoming.MessageID
		}
	}
}

func (m *Manager) dispatchOutgoing(ctx context.Context, out OutgoingMessage) error {
	m.bus.PublishOutbound(ctx, out)

	m.mu.RLock()
	ch := m.channels[out.Channel]
	m.mu.RUnlock()
	if ch == nil {
		return nil
	}
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
		var topicIDPtr *string
		if msg.TopicID != "" {
			topicIDPtr = &msg.TopicID
		}
		m.store.Remove(msg.Channel, msg.ChatID, topicIDPtr)
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
			TopicID: msg.TopicID,
			Text:    response,
			IsFinal: true,
		}
		m.enrichOutgoing(&out, msg, msg.ThreadID)
		return m.dispatchOutgoing(ctx, out)
	}
	return nil
}
