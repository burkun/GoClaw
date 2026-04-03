package channels

import (
	"context"
	"testing"
)

type mockChannel struct {
	name    string
	handler MessageHandler
	sent    []OutgoingMessage
}

func (m *mockChannel) Name() string { return m.name }
func (m *mockChannel) Start(_ context.Context, h MessageHandler) error {
	m.handler = h
	return nil
}
func (m *mockChannel) Stop(_ context.Context) error { return nil }
func (m *mockChannel) Send(_ context.Context, msg OutgoingMessage) error {
	m.sent = append(m.sent, msg)
	return nil
}

func TestManager_Dispatch(t *testing.T) {
	proc := ProcessorFunc(func(_ context.Context, msg IncomingMessage, threadID string) (OutgoingMessage, error) {
		return OutgoingMessage{Text: "echo: " + msg.Text, ThreadID: threadID}, nil
	})
	mgr := NewManager(nil, proc)
	mgr.SetThreadIDGenerator(func(msg IncomingMessage) string { return "thread-fixed-" + msg.ChatID })

	ch := &mockChannel{name: "feishu"}
	if err := mgr.RegisterChannel(ch); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	if ch.handler == nil {
		t.Fatal("handler not wired")
	}
	if err := ch.handler(context.Background(), IncomingMessage{Channel: "feishu", ChatID: "chat-1", Text: "hello"}); err != nil {
		t.Fatalf("incoming failed: %v", err)
	}

	if len(ch.sent) != 1 {
		t.Fatalf("expected one outbound message, got %d", len(ch.sent))
	}
	if ch.sent[0].Text != "echo: hello" {
		t.Fatalf("unexpected outbound text: %q", ch.sent[0].Text)
	}
	if ch.sent[0].ThreadID != "thread-fixed-chat-1" {
		t.Fatalf("unexpected thread id: %q", ch.sent[0].ThreadID)
	}
}

func TestManager_BusPublish(t *testing.T) {
	mgr := NewManager(nil, nil)
	mgr.SetThreadIDGenerator(func(msg IncomingMessage) string { return "thread-" + msg.ChatID })
	ch := &mockChannel{name: "slack"}
	_ = mgr.RegisterChannel(ch)
	_ = mgr.Start(context.Background())

	sub, unsub := mgr.Bus().Subscribe(1)
	defer unsub()

	if err := ch.handler(context.Background(), IncomingMessage{Channel: "slack", ChatID: "room-7", Text: "ping"}); err != nil {
		t.Fatalf("incoming failed: %v", err)
	}

	msg := <-sub
	if msg.ThreadID != "thread-room-7" {
		t.Fatalf("unexpected thread id on bus message: %q", msg.ThreadID)
	}
}
