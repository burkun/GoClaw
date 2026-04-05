package channels

import (
	"context"
	"sync"
)

// OutboundHandler handles one outbound message event.
type OutboundHandler func(ctx context.Context, msg OutgoingMessage) error

// MessageBus provides lightweight pub/sub for channel events.
type MessageBus struct {
	mu          sync.RWMutex
	subscribers map[int]chan IncomingMessage
	nextID      int

	outboundMu          sync.RWMutex
	outboundSubscribers map[int]OutboundHandler
	outboundNextID      int
}

// NewMessageBus creates an empty bus.
func NewMessageBus() *MessageBus {
	return &MessageBus{
		subscribers:         make(map[int]chan IncomingMessage),
		outboundSubscribers: make(map[int]OutboundHandler),
	}
}

// Subscribe registers a new subscriber.
func (b *MessageBus) Subscribe(buffer int) (<-chan IncomingMessage, func()) {
	if buffer < 0 {
		buffer = 0
	}
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	ch := make(chan IncomingMessage, buffer)
	b.subscribers[id] = ch
	b.mu.Unlock()

	unsub := func() {
		b.mu.Lock()
		if c, ok := b.subscribers[id]; ok {
			delete(b.subscribers, id)
			close(c)
		}
		b.mu.Unlock()
	}
	return ch, unsub
}

// Publish broadcasts one message to all current subscribers.
func (b *MessageBus) Publish(msg IncomingMessage) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subscribers {
		select {
		case ch <- msg:
		default:
			// Drop when subscriber channel is full.
		}
	}
}

// SubscribeOutbound registers an outbound message subscriber.
func (b *MessageBus) SubscribeOutbound(handler OutboundHandler) func() {
	if handler == nil {
		return func() {}
	}
	b.outboundMu.Lock()
	id := b.outboundNextID
	b.outboundNextID++
	b.outboundSubscribers[id] = handler
	b.outboundMu.Unlock()

	return func() {
		b.outboundMu.Lock()
		delete(b.outboundSubscribers, id)
		b.outboundMu.Unlock()
	}
}

// PublishOutbound broadcasts one outbound message to all subscribers.
// Each subscriber runs in its own goroutine to avoid blocking sender path.
func (b *MessageBus) PublishOutbound(ctx context.Context, msg OutgoingMessage) {
	b.outboundMu.RLock()
	handlers := make([]OutboundHandler, 0, len(b.outboundSubscribers))
	for _, handler := range b.outboundSubscribers {
		handlers = append(handlers, handler)
	}
	b.outboundMu.RUnlock()

	for _, handler := range handlers {
		h := handler
		go func() {
			_ = h(ctx, msg)
		}()
	}
}
