package channels

import "sync"

// MessageBus provides lightweight pub/sub for channel events.
type MessageBus struct {
	mu          sync.RWMutex
	subscribers map[int]chan IncomingMessage
	nextID      int
}

// NewMessageBus creates an empty bus.
func NewMessageBus() *MessageBus {
	return &MessageBus{subscribers: make(map[int]chan IncomingMessage)}
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
