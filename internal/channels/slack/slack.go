package slack

import (
	"context"
	"fmt"
	"sync"

	"github.com/bookerbai/goclaw/internal/channels"
)

// Channel is a minimal Slack adapter skeleton.
type Channel struct {
	mu      sync.RWMutex
	started bool
	handler channels.MessageHandler
}

func New() *Channel { return &Channel{} }

func (c *Channel) Name() string { return "slack" }

func (c *Channel) Start(_ context.Context, handler channels.MessageHandler) error {
	if handler == nil {
		return fmt.Errorf("slack: handler is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handler = handler
	c.started = true
	return nil
}

func (c *Channel) Stop(_ context.Context) error {
	c.mu.Lock()
	c.started = false
	c.handler = nil
	c.mu.Unlock()
	return nil
}

func (c *Channel) Send(_ context.Context, _ channels.OutgoingMessage) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.started {
		return fmt.Errorf("slack: channel not started")
	}
	return nil
}

// InjectIncoming is a test/helper entry to simulate inbound events.
func (c *Channel) InjectIncoming(ctx context.Context, msg channels.IncomingMessage) error {
	c.mu.RLock()
	h := c.handler
	started := c.started
	c.mu.RUnlock()
	if !started || h == nil {
		return fmt.Errorf("slack: channel not started")
	}
	if msg.Channel == "" {
		msg.Channel = c.Name()
	}
	return h(ctx, msg)
}

var _ channels.Channel = (*Channel)(nil)
