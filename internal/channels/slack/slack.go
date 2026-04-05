// Package slack provides a Slack API channel adapter for GoClaw.
package slack

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/bookerbai/goclaw/internal/channels"
	"github.com/bookerbai/goclaw/internal/config"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// Channel is a Slack adapter for GoClaw using Socket Mode.
type Channel struct {
	mu       sync.RWMutex
	started  bool
	handler  channels.MessageHandler
	client   *slack.Client
	socket   *socketmode.Client
	cancel   context.CancelFunc
	cfg      config.SlackConfig
	messages map[string][]slack.Message // ThreadID -> messages history
	botID    string
}

// New creates a new Slack channel adapter.
func New(cfg config.SlackConfig) *Channel {
	return &Channel{
		cfg:      cfg,
		messages: make(map[string][]slack.Message),
	}
}

// Name returns the channel name.
func (c *Channel) Name() string { return "slack" }

// Start initializes the Slack client and starts Socket Mode.
func (c *Channel) Start(ctx context.Context, handler channels.MessageHandler) error {
	if handler == nil {
		return fmt.Errorf("slack: handler is nil")
	}

	botToken := strings.TrimSpace(c.cfg.BotToken)
	appToken := strings.TrimSpace(c.cfg.AppToken)

	if botToken == "" {
		return fmt.Errorf("slack: bot token is required")
	}
	if appToken == "" {
		return fmt.Errorf("slack: app token is required for Socket Mode")
	}

	// Create Slack client
	client := slack.New(
		botToken,
		slack.OptionAppLevelToken(appToken),
		slack.OptionDebug(false),
	)

	// Get bot info
	authTest, err := client.AuthTest()
	if err != nil {
		return fmt.Errorf("slack: failed to authenticate: %w", err)
	}
	c.botID = authTest.UserID
	log.Printf("[Slack] Connected as %s (ID: %s)", authTest.User, c.botID)

	// Create Socket Mode client
	socket := socketmode.New(
		client,
		socketmode.OptionDebug(false),
	)

	ctx, cancel := context.WithCancel(ctx)

	c.mu.Lock()
	c.handler = handler
	c.client = client
	c.socket = socket
	c.cancel = cancel
	c.started = true
	c.mu.Unlock()

	// Start Socket Mode in a goroutine
	go c.runSocketMode(ctx)

	return nil
}

// runSocketMode handles Socket Mode events.
func (c *Channel) runSocketMode(ctx context.Context) {
	c.mu.RLock()
	socket := c.socket
	c.mu.RUnlock()

	if socket == nil {
		return
	}

	go func() {
		if err := socket.Run(); err != nil {
			log.Printf("[Slack] Socket mode error: %v", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-socket.Events:
			switch evt.Type {
			case socketmode.EventTypeConnecting:
				log.Println("[Slack] Connecting...")
			case socketmode.EventTypeConnectionError:
				log.Println("[Slack] Connection error")
			case socketmode.EventTypeConnected:
				log.Println("[Slack] Connected!")
			case socketmode.EventTypeEventsAPI:
				eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				socket.Ack(*evt.Request)
				if err := c.handleEventsAPI(ctx, eventsAPIEvent); err != nil {
					log.Printf("[Slack] Error handling event: %v", err)
				}
			}
		}
	}
}

// handleEventsAPI processes Slack Events API events.
func (c *Channel) handleEventsAPI(ctx context.Context, event slackevents.EventsAPIEvent) error {
	switch event.Type {
	case slackevents.CallbackEvent:
		innerEvent := event.InnerEvent
		switch ev := innerEvent.Data.(type) {
		case *slackevents.MessageEvent:
			// Ignore bot's own messages
			if ev.BotID != "" || ev.User == c.botID {
				return nil
			}
			return c.handleMessage(ctx, ev)
		case *slackevents.AppMentionEvent:
			return c.handleAppMention(ctx, ev)
		}
	}
	return nil
}

// handleMessage processes a message event.
func (c *Channel) handleMessage(ctx context.Context, ev *slackevents.MessageEvent) error {
	threadID := ev.Channel
	userID := ev.User
	content := ev.Text

	// Store message
	c.mu.Lock()
	c.messages[threadID] = append(c.messages[threadID], slack.Message{
		Msg: slack.Msg{
			Text:      ev.Text,
			Timestamp: ev.TimeStamp,
			User:      ev.User,
			Channel:   ev.Channel,
		},
	})
	c.mu.Unlock()

	incoming := channels.IncomingMessage{
		Channel:  c.Name(),
		ChatID:   threadID,
		UserID:   userID,
		Text:     content,
		ThreadID: threadID,
		Metadata: map[string]any{
			"username": ev.Username,
			"raw":      ev,
		},
	}

	c.mu.RLock()
	handler := c.handler
	c.mu.RUnlock()

	if handler != nil {
		return handler(ctx, incoming)
	}
	return nil
}

// handleAppMention processes an app mention event.
func (c *Channel) handleAppMention(ctx context.Context, ev *slackevents.AppMentionEvent) error {
	threadID := ev.Channel
	userID := ev.User
	content := ev.Text

	incoming := channels.IncomingMessage{
		Channel:  c.Name(),
		ChatID:   threadID,
		UserID:   userID,
		Text:     content,
		ThreadID: threadID,
		Metadata: map[string]any{
			"raw": ev,
		},
	}

	c.mu.RLock()
	handler := c.handler
	c.mu.RUnlock()

	if handler != nil {
		return handler(ctx, incoming)
	}
	return nil
}

// Stop stops the Slack Socket Mode.
func (c *Channel) Stop(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return nil
	}

	if c.cancel != nil {
		c.cancel()
	}

	c.started = false
	c.handler = nil
	c.client = nil
	c.socket = nil
	return nil
}

// Send sends an outgoing message to Slack.
func (c *Channel) Send(ctx context.Context, msg channels.OutgoingMessage) error {
	c.mu.RLock()
	client := c.client
	started := c.started
	c.mu.RUnlock()

	if !started {
		return fmt.Errorf("slack: channel not started")
	}
	if client == nil {
		return fmt.Errorf("slack: client not initialized")
	}

	_, _, err := client.PostMessageContext(ctx, msg.ThreadID,
		slack.MsgOptionText(msg.Text, false),
	)
	if err != nil {
		return fmt.Errorf("slack: failed to send message: %w", err)
	}

	return nil
}

// SendFile sends a file to Slack.
func (c *Channel) SendFile(ctx context.Context, threadID string, filename string, content []byte) error {
	c.mu.RLock()
	client := c.client
	started := c.started
	c.mu.RUnlock()

	if !started {
		return fmt.Errorf("slack: channel not started")
	}
	if client == nil {
		return fmt.Errorf("slack: client not initialized")
	}

	_, err := client.UploadFileContext(ctx, slack.UploadFileParameters{
		Channel:  threadID,
		Filename: filename,
		Content:  string(content),
	})
	if err != nil {
		return fmt.Errorf("slack: failed to upload file: %w", err)
	}

	return nil
}

// GetChatHistory returns the message history for a channel.
func (c *Channel) GetChatHistory(threadID string) []slack.Message {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.messages[threadID]
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
