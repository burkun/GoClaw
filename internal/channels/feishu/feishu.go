// Package feishu provides a Feishu/Lark Bot API channel adapter for GoClaw.
package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bookerbai/goclaw/internal/channels"
	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/internal/logging"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// Channel is a Feishu/Lark Bot API adapter for GoClaw.
type Channel struct {
	mu             sync.RWMutex
	started        bool
	handler        channels.MessageHandler
	client         *lark.Client
	cancel         context.CancelFunc
	cfg            config.FeishuConfig
	messages       map[string][]*larkim.Message // ChatID -> messages history
	runningCardIDs map[string]string            // source_message_id -> running card message_id
	wg             sync.WaitGroup               // tracks active goroutines
}

// New creates a new Feishu channel adapter.
func New(cfg config.FeishuConfig) *Channel {
	return &Channel{
		cfg:            cfg,
		messages:       make(map[string][]*larkim.Message),
		runningCardIDs: make(map[string]string),
	}
}

// Name returns the channel name.
func (c *Channel) Name() string { return "feishu" }

// Start initializes the Feishu client and starts event handling.
func (c *Channel) Start(ctx context.Context, handler channels.MessageHandler) error {
	if handler == nil {
		return fmt.Errorf("feishu: handler is nil")
	}

	appID := strings.TrimSpace(c.cfg.AppID)
	appSecret := strings.TrimSpace(c.cfg.AppSecret)

	if appID == "" {
		return fmt.Errorf("feishu: app_id is required")
	}
	if appSecret == "" {
		return fmt.Errorf("feishu: app_secret is required")
	}

	// Create Feishu client
	client := lark.NewClient(appID, appSecret,
		lark.WithEnableTokenCache(true),
	)

	// Test authentication - the client auto-manages token, just verify connectivity
	logging.Info("[Feishu] Client initialized", "app_id", appID)

	ctx, cancel := context.WithCancel(ctx)

	c.mu.Lock()
	c.handler = handler
	c.client = client
	c.cancel = cancel
	c.started = true
	c.mu.Unlock()

	// Start webhook server if configured
	if c.cfg.WebhookPort > 0 {
		go c.startWebhookServer(ctx)
	}

	return nil
}

// startWebhookServer starts an HTTP server to receive Feishu events.
func (c *Channel) startWebhookServer(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook/feishu", c.handleWebhook)

	addr := fmt.Sprintf(":%d", c.cfg.WebhookPort)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	logging.Info("[Feishu] Starting webhook server", "address", addr)

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logging.Error("[Feishu] Webhook server error", "error", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logging.Error("[Feishu] Webhook server shutdown error", "error", err)
	}
}

// handleWebhook handles incoming Feishu webhook events.
func (c *Channel) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var event FeishuEvent
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Handle URL verification
	if event.Type == "url_verification" {
		resp := map[string]string{
			"challenge": event.Challenge,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	// Handle event callback
	if event.Type == "event_callback" {
		if err := c.processEvent(r.Context(), event.Event); err != nil {
			logging.Error("[Feishu] Error processing event", "error", err)
		}
	}

	w.WriteHeader(http.StatusOK)
}

// FeishuEvent represents a Feishu webhook event.
type FeishuEvent struct {
	Type      string          `json:"type"`
	Challenge string          `json:"challenge,omitempty"`
	Token     string          `json:"token,omitempty"`
	Event     json.RawMessage `json:"event,omitempty"`
}

// FeishuMessageEvent represents a message event.
type FeishuMessageEvent struct {
	Sender struct {
		SenderID struct {
			UnionID string `json:"union_id"`
			UserID  string `json:"user_id"`
			OpenID  string `json:"open_id"`
		} `json:"sender_id"`
	} `json:"sender"`
	Message struct {
		MessageID   string `json:"message_id"`
		RootID      string `json:"root_id,omitempty"`
		Content     string `json:"content"`
		MessageType string `json:"message_type"`
		ChatID      string `json:"chat_id"`
		ChatType    string `json:"chat_type"`
	} `json:"message"`
}

// processEvent processes a Feishu event.
func (c *Channel) processEvent(ctx context.Context, rawEvent json.RawMessage) error {
	var msgEvent FeishuMessageEvent
	if err := json.Unmarshal(rawEvent, &msgEvent); err != nil {
		return err
	}

	// Skip non-text messages
	if msgEvent.Message.MessageType != "text" {
		return nil
	}

	// Parse content (it's a JSON string containing the actual content)
	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(msgEvent.Message.Content), &content); err != nil {
		// Fallback: use raw content
		content.Text = msgEvent.Message.Content
	}

	chatID := msgEvent.Message.ChatID
	userID := msgEvent.Sender.SenderID.OpenID
	rootID := strings.TrimSpace(msgEvent.Message.RootID)

	incoming := channels.IncomingMessage{
		Channel:   c.Name(),
		ChatID:    chatID,
		TopicID:   rootID,
		UserID:    userID,
		Text:      content.Text,
		MessageID: msgEvent.Message.MessageID,
		ThreadID:  chatID,
		Metadata: map[string]any{
			"raw":               msgEvent,
			"source_message_id": msgEvent.Message.MessageID,
			"thread_ts":         msgEvent.Message.MessageID,
			"root_id":           rootID,
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

// Stop stops the Feishu channel.
func (c *Channel) Stop(_ context.Context) error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return nil
	}

	if c.cancel != nil {
		c.cancel()
	}
	c.started = false
	c.handler = nil
	c.client = nil
	c.runningCardIDs = make(map[string]string)
	c.mu.Unlock()

	// Wait for goroutines to exit after releasing lock
	c.wg.Wait()
	return nil
}

// Send sends an outgoing message to Feishu with retry support.
// It mirrors DeerFlow's behavior: when source_message_id exists, update a running card;
// otherwise create a new card in chat.
func (c *Channel) Send(ctx context.Context, msg channels.OutgoingMessage) error {
	return channels.Retry(ctx, channels.DefaultRetryConfig(), func() error {
		return c.sendOnce(ctx, msg)
	})
}

// sendOnce is the internal implementation of Send without retry.
func (c *Channel) sendOnce(ctx context.Context, msg channels.OutgoingMessage) error {
	c.mu.RLock()
	client := c.client
	started := c.started
	c.mu.RUnlock()

	if !started {
		return fmt.Errorf("feishu: channel not started")
	}
	if client == nil {
		return fmt.Errorf("feishu: client not initialized")
	}

	sourceMessageID := sourceMessageIDFromMetadata(msg.Metadata)
	if sourceMessageID != "" {
		if err := c.sendOrUpdateRunningCard(ctx, client, sourceMessageID, msg.Text, msg.IsFinal); err != nil {
			return err
		}
		if msg.IsFinal {
			_ = c.addReaction(ctx, client, sourceMessageID, "DONE")
			c.removeRunningCard(sourceMessageID)
		}
		return nil
	}

	targetChatID := strings.TrimSpace(msg.ChatID)
	if targetChatID == "" {
		targetChatID = strings.TrimSpace(msg.ThreadID)
	}
	if targetChatID == "" {
		return fmt.Errorf("feishu: chat id is required")
	}

	_, err := c.createCard(ctx, client, targetChatID, msg.Text)
	return err
}

func sourceMessageIDFromMetadata(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	for _, k := range []string{"source_message_id", "thread_ts", "message_id"} {
		if v, ok := metadata[k]; ok {
			if s, ok := v.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func buildCardContent(text string) string {
	card := map[string]any{
		"config": map[string]any{
			"wide_screen_mode": true,
			"update_multi":     true,
		},
		"elements": []map[string]any{
			{
				"tag":     "markdown",
				"content": text,
			},
		},
	}
	b, _ := json.Marshal(card)
	return string(b)
}

func (c *Channel) getRunningCard(sourceMessageID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.runningCardIDs == nil {
		return ""
	}
	return c.runningCardIDs[sourceMessageID]
}

func (c *Channel) setRunningCard(sourceMessageID, runningCardID string) {
	if sourceMessageID == "" || runningCardID == "" {
		return
	}
	c.mu.Lock()
	if c.runningCardIDs == nil {
		c.runningCardIDs = make(map[string]string)
	}
	c.runningCardIDs[sourceMessageID] = runningCardID
	c.mu.Unlock()
}

func (c *Channel) removeRunningCard(sourceMessageID string) {
	if sourceMessageID == "" {
		return
	}
	c.mu.Lock()
	if c.runningCardIDs != nil {
		delete(c.runningCardIDs, sourceMessageID)
	}
	c.mu.Unlock()
}

func (c *Channel) createCard(ctx context.Context, client *lark.Client, chatID, text string) (string, error) {
	content := buildCardContent(text)
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType("chat_id").
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType("interactive").
			Content(content).
			Build()).
		Build()

	resp, err := client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("feishu: failed to create card: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("feishu: create card failed: %s", resp.Msg)
	}
	if resp.Data != nil && resp.Data.MessageId != nil {
		return strings.TrimSpace(*resp.Data.MessageId), nil
	}
	return "", nil
}

func (c *Channel) replyCard(ctx context.Context, client *lark.Client, sourceMessageID, text string) (string, error) {
	content := buildCardContent(text)
	req := larkim.NewReplyMessageReqBuilder().
		MessageId(sourceMessageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType("interactive").
			Content(content).
			ReplyInThread(true).
			Build()).
		Build()

	resp, err := client.Im.V1.Message.Reply(ctx, req)
	if err != nil {
		return "", fmt.Errorf("feishu: failed to reply card: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("feishu: reply card failed: %s", resp.Msg)
	}
	if resp.Data != nil && resp.Data.MessageId != nil {
		return strings.TrimSpace(*resp.Data.MessageId), nil
	}
	return "", nil
}

func (c *Channel) updateCard(ctx context.Context, client *lark.Client, runningCardID, text string) error {
	content := buildCardContent(text)
	req := larkim.NewPatchMessageReqBuilder().
		MessageId(runningCardID).
		Body(larkim.NewPatchMessageReqBodyBuilder().
			Content(content).
			Build()).
		Build()

	resp, err := client.Im.V1.Message.Patch(ctx, req)
	if err != nil {
		return fmt.Errorf("feishu: failed to patch card: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu: patch card failed: %s", resp.Msg)
	}
	return nil
}

func (c *Channel) addReaction(ctx context.Context, client *lark.Client, sourceMessageID, emojiType string) error {
	req := larkim.NewCreateMessageReactionReqBuilder().
		MessageId(sourceMessageID).
		Body(larkim.NewCreateMessageReactionReqBodyBuilder().
			ReactionType(larkim.NewEmojiBuilder().EmojiType(emojiType).Build()).
			Build()).
		Build()

	resp, err := client.Im.V1.MessageReaction.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("feishu: add reaction failed: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu: add reaction failed: %s", resp.Msg)
	}
	return nil
}

func (c *Channel) sendOrUpdateRunningCard(ctx context.Context, client *lark.Client, sourceMessageID, text string, isFinal bool) error {
	runningCardID := c.getRunningCard(sourceMessageID)
	if runningCardID == "" {
		newCardID, err := c.replyCard(ctx, client, sourceMessageID, text)
		if err != nil {
			return err
		}
		if newCardID != "" {
			c.setRunningCard(sourceMessageID, newCardID)
		}
		return nil
	}

	if err := c.updateCard(ctx, client, runningCardID, text); err != nil {
		if !isFinal {
			return err
		}
		// For final message, fallback to reply when patch failed.
		_, replyErr := c.replyCard(ctx, client, sourceMessageID, text)
		if replyErr != nil {
			return replyErr
		}
	}
	return nil
}

// SendFile sends a file to Feishu.
func (c *Channel) SendFile(ctx context.Context, threadID string, filename string, content []byte) error {
	c.mu.RLock()
	client := c.client
	started := c.started
	c.mu.RUnlock()

	if !started {
		return fmt.Errorf("feishu: channel not started")
	}
	if client == nil {
		return fmt.Errorf("feishu: client not initialized")
	}

	// First upload the file
	fileReq := larkim.NewCreateFileReqBuilder().
		Body(larkim.NewCreateFileReqBodyBuilder().
			FileType("stream").
			FileName(filename).
			File(bytes.NewReader(content)).
			Build()).
		Build()

	fileResp, err := client.Im.V1.File.Create(ctx, fileReq)
	if err != nil {
		return fmt.Errorf("feishu: failed to upload file: %w", err)
	}
	if !fileResp.Success() {
		return fmt.Errorf("feishu: upload file failed: %s", fileResp.Msg)
	}

	fileKey := *fileResp.Data.FileKey

	// Send file message
	fileContent := map[string]string{
		"file_key": fileKey,
	}
	contentBytes, _ := json.Marshal(fileContent)

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType("chat_id").
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(threadID).
			MsgType("file").
			Content(string(contentBytes)).
			Build()).
		Build()

	resp, err := client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("feishu: failed to send file: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu: send file failed: %s", resp.Msg)
	}

	return nil
}

// GetChatHistory returns the message history for a chat.
func (c *Channel) GetChatHistory(threadID string) []*larkim.Message {
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
		return fmt.Errorf("feishu: channel not started")
	}
	if msg.Channel == "" {
		msg.Channel = c.Name()
	}
	return h(ctx, msg)
}

var _ channels.Channel = (*Channel)(nil)
