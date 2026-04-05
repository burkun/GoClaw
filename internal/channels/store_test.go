package channels

import "testing"

func TestInMemoryChannelStore_Basic(t *testing.T) {
	s := NewInMemoryChannelStore()

	if _, ok := s.GetThreadID("feishu", "chat-1", nil); ok {
		t.Fatal("unexpected mapping before set")
	}

	s.SetThreadID("feishu", "chat-1", "thread-1", nil, "")
	threadID, ok := s.GetThreadID("feishu", "chat-1", nil)
	if !ok || threadID != "thread-1" {
		t.Fatalf("unexpected mapping: ok=%v thread=%s", ok, threadID)
	}

	s.Remove("feishu", "chat-1", nil)
	if _, ok := s.GetThreadID("feishu", "chat-1", nil); ok {
		t.Fatal("expected mapping removed")
	}
}
