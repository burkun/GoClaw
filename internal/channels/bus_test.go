package channels

import (
	"testing"
	"time"
)

func TestMessageBus_PublishSubscribe(t *testing.T) {
	bus := NewMessageBus()
	ch, unsub := bus.Subscribe(1)
	defer unsub()

	bus.Publish(IncomingMessage{Channel: "feishu", ChatID: "c1", Text: "hello"})

	select {
	case msg := <-ch:
		if msg.Text != "hello" {
			t.Fatalf("unexpected msg: %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting message")
	}
}

func TestMessageBus_Unsubscribe(t *testing.T) {
	bus := NewMessageBus()
	_, unsub := bus.Subscribe(1)
	unsub()
	// Should not panic.
	bus.Publish(IncomingMessage{Channel: "slack", ChatID: "c2", Text: "x"})
}
