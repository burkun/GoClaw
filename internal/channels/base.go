package channels

import "context"

// IncomingMessage is a normalized inbound message from any IM channel.
type IncomingMessage struct {
	Channel   string         `json:"channel"`
	ChatID    string         `json:"chat_id"`
	UserID    string         `json:"user_id,omitempty"`
	Text      string         `json:"text,omitempty"`
	ThreadID  string         `json:"thread_id,omitempty"`
	MessageID string         `json:"message_id,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// OutgoingMessage is a normalized outbound message to any IM channel.
type OutgoingMessage struct {
	Channel   string         `json:"channel"`
	ChatID    string         `json:"chat_id"`
	ThreadID  string         `json:"thread_id,omitempty"`
	Text      string         `json:"text,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// MessageHandler consumes incoming channel messages.
type MessageHandler func(ctx context.Context, msg IncomingMessage) error

// Channel defines the minimal lifecycle and send contract for an IM adapter.
type Channel interface {
	Name() string
	Start(ctx context.Context, handler MessageHandler) error
	Stop(ctx context.Context) error
	Send(ctx context.Context, msg OutgoingMessage) error
}

// Processor runs agent logic for one incoming message.
type Processor interface {
	Process(ctx context.Context, msg IncomingMessage, threadID string) (OutgoingMessage, error)
}

// ProcessorFunc adapts a function to Processor.
type ProcessorFunc func(ctx context.Context, msg IncomingMessage, threadID string) (OutgoingMessage, error)

func (f ProcessorFunc) Process(ctx context.Context, msg IncomingMessage, threadID string) (OutgoingMessage, error) {
	return f(ctx, msg, threadID)
}
