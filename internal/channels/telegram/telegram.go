// Package telegram provides a Telegram Bot API channel adapter for GoClaw.
package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/bookerbai/goclaw/internal/channels"
	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/internal/logging"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Channel is a Telegram Bot API adapter for GoClaw.
type Channel struct {
	mu       sync.RWMutex
	started  bool
	handler  channels.MessageHandler
	bot      *tgbotapi.BotAPI
	cancel   context.CancelFunc
	cfg      config.TelegramConfig
	messages map[string][]tgbotapi.Message // ThreadID -> messages history
}

// New creates a new Telegram channel adapter.
func New(cfg config.TelegramConfig) *Channel {
	return &Channel{
		cfg:      cfg,
		messages: make(map[string][]tgbotapi.Message),
	}
}

// Name returns the channel name.
func (c *Channel) Name() string { return "telegram" }

// Start initializes the Telegram bot and starts polling for updates.
func (c *Channel) Start(ctx context.Context, handler channels.MessageHandler) error {
	if handler == nil {
		return fmt.Errorf("telegram: handler is nil")
	}

	token := strings.TrimSpace(c.cfg.BotToken)
	if token == "" {
		return fmt.Errorf("telegram: bot token is required")
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return fmt.Errorf("telegram: failed to create bot: %w", err)
	}

	bot.Debug = false
	logging.Info("[Telegram] Authorized on account", "username", bot.Self.UserName)

	ctx, cancel := context.WithCancel(ctx)

	c.mu.Lock()
	c.handler = handler
	c.bot = bot
	c.cancel = cancel
	c.started = true
	c.mu.Unlock()

	// Start polling in a goroutine
	go c.pollUpdates(ctx)

	return nil
}

// pollUpdates continuously polls for Telegram updates.
func (c *Channel) pollUpdates(ctx context.Context) {
	c.mu.RLock()
	bot := c.bot
	c.mu.RUnlock()

	if bot == nil {
		return
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			if err := c.handleUpdate(ctx, update); err != nil {
				logging.Error("[Telegram] Error handling update", "error", err)
			}
		}
	}
}

// handleUpdate processes a single Telegram update.
func (c *Channel) handleUpdate(ctx context.Context, update tgbotapi.Update) error {
	if update.Message == nil {
		return nil // No message to handle
	}

	msg := update.Message
	threadID := strconv.FormatInt(msg.Chat.ID, 10)
	userID := strconv.FormatInt(msg.From.ID, 10)

	// Build incoming message
	incoming := channels.IncomingMessage{
		Channel:  c.Name(),
		ChatID:   threadID,
		UserID:   userID,
		Text:     msg.Text,
		ThreadID: threadID,
		Metadata: map[string]any{
			"username": msg.From.UserName,
			"raw":      msg,
		},
	}

	// Store message for context
	c.mu.Lock()
	c.messages[threadID] = append(c.messages[threadID], *msg)
	c.mu.Unlock()

	c.mu.RLock()
	handler := c.handler
	c.mu.RUnlock()

	if handler != nil {
		return handler(ctx, incoming)
	}
	return nil
}

// Stop stops the Telegram bot polling.
func (c *Channel) Stop(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return nil
	}

	if c.cancel != nil {
		c.cancel()
	}

	if c.bot != nil {
		c.bot.StopReceivingUpdates()
	}

	c.started = false
	c.handler = nil
	c.bot = nil
	return nil
}

// Send sends an outgoing message to Telegram with retry support.
func (c *Channel) Send(ctx context.Context, msg channels.OutgoingMessage) error {
	return channels.Retry(ctx, channels.DefaultRetryConfig(), func() error {
		return c.sendOnce(ctx, msg)
	})
}

// sendOnce is the internal implementation of Send without retry.
func (c *Channel) sendOnce(ctx context.Context, msg channels.OutgoingMessage) error {
	c.mu.RLock()
	bot := c.bot
	started := c.started
	c.mu.RUnlock()

	if !started {
		return fmt.Errorf("telegram: channel not started")
	}
	if bot == nil {
		return fmt.Errorf("telegram: bot not initialized")
	}

	chatID, err := strconv.ParseInt(msg.ThreadID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: invalid chat ID: %w", err)
	}

	tgMsg := tgbotapi.NewMessage(chatID, msg.Text)
	// Disable link preview if needed
	tgMsg.DisableWebPagePreview = true

	_, err = bot.Send(tgMsg)
	if err != nil {
		return fmt.Errorf("telegram: failed to send message: %w", err)
	}

	return nil
}

// SendFile sends a file to Telegram.
func (c *Channel) SendFile(ctx context.Context, threadID string, filename string, content []byte) error {
	c.mu.RLock()
	bot := c.bot
	started := c.started
	c.mu.RUnlock()

	if !started {
		return fmt.Errorf("telegram: channel not started")
	}
	if bot == nil {
		return fmt.Errorf("telegram: bot not initialized")
	}

	chatID, err := strconv.ParseInt(threadID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: invalid chat ID: %w", err)
	}

	// Create a document upload
	file := tgbotapi.FileBytes{
		Name:  filename,
		Bytes: content,
	}

	doc := tgbotapi.NewDocument(chatID, file)
	_, err = bot.Send(doc)
	if err != nil {
		return fmt.Errorf("telegram: failed to send file: %w", err)
	}

	return nil
}

// GetChatHistory returns the message history for a chat.
func (c *Channel) GetChatHistory(threadID string) []tgbotapi.Message {
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
		return fmt.Errorf("telegram: channel not started")
	}
	if msg.Channel == "" {
		msg.Channel = c.Name()
	}
	return h(ctx, msg)
}

var _ channels.Channel = (*Channel)(nil)
