package memory

import (
	"testing"
)

func TestDetectCorrection(t *testing.T) {
	tests := []struct {
		name     string
		messages []map[string]any
		expected bool
	}{
		{
			name:     "empty messages",
			messages: []map[string]any{},
			expected: false,
		},
		{
			name: "no correction",
			messages: []map[string]any{
				{"role": "human", "content": "hello"},
				{"role": "assistant", "content": "hi"},
			},
			expected: false,
		},
		{
			name: "that's wrong",
			messages: []map[string]any{
				{"role": "human", "content": "that's wrong, try again"},
			},
			expected: true,
		},
		{
			name: "you misunderstood",
			messages: []map[string]any{
				{"role": "human", "content": "you misunderstood me"},
			},
			expected: true,
		},
		{
			name: "Chinese correction",
			messages: []map[string]any{
				{"role": "human", "content": "不对，你理解错了"},
			},
			expected: true,
		},
		{
			name: "correction in last 6 messages",
			messages: []map[string]any{
				{"role": "human", "content": "msg1"},
				{"role": "human", "content": "msg2"},
				{"role": "human", "content": "msg3"},
				{"role": "human", "content": "msg4"},
				{"role": "human", "content": "msg5"},
				{"role": "human", "content": "msg6"},
				{"role": "human", "content": "that's wrong"},
			},
			expected: true,
		},
		{
			name: "correction beyond last 6 messages",
			messages: []map[string]any{
				{"role": "human", "content": "that's wrong"},
				{"role": "human", "content": "msg2"},
				{"role": "human", "content": "msg3"},
				{"role": "human", "content": "msg4"},
				{"role": "human", "content": "msg5"},
				{"role": "human", "content": "msg6"},
				{"role": "human", "content": "msg7"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectCorrection(tt.messages)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestFilterMessagesForMemory(t *testing.T) {
	tests := []struct {
		name     string
		input    []map[string]any
		expected int
	}{
		{
			name:     "empty messages",
			input:    []map[string]any{},
			expected: 0,
		},
		{
			name: "filter tool messages",
			input: []map[string]any{
				{"role": "human", "content": "hello"},
				{"role": "tool", "content": "result"},
				{"role": "assistant", "content": "hi"},
			},
			expected: 2,
		},
		{
			name: "filter assistant with tool calls",
			input: []map[string]any{
				{"role": "human", "content": "hello"},
				{"role": "assistant", "content": "let me check", "tool_calls": []map[string]any{{"name": "read"}}},
				{"role": "assistant", "content": "final answer"},
			},
			expected: 2,
		},
		{
			name: "strip uploaded files block",
			input: []map[string]any{
				{"role": "human", "content": "<uploaded_files>\nfile content\n</uploaded_files>\nactual message"},
			},
			expected: 1,
		},
		{
			name: "empty after strip",
			input: []map[string]any{
				{"role": "human", "content": "<uploaded_files>\nfile content\n</uploaded_files>"},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterMessagesForMemory(tt.input)
			if len(result) != tt.expected {
				t.Errorf("expected %d messages, got %d", tt.expected, len(result))
			}
		})
	}
}

func TestDeriveFactsFromMessages(t *testing.T) {
	tests := []struct {
		name              string
		messages          []map[string]any
		correction        bool
		expectFacts       bool
		minFacts          int
		expectCorrection  bool
	}{
		{
			name:        "empty messages",
			messages:    []map[string]any{},
			correction:  false,
			expectFacts: false,
		},
		{
			name: "with correction",
			messages: []map[string]any{
				{"role": "human", "content": "hello"},
				{"role": "assistant", "content": "hi"},
			},
			correction:       true,
			expectFacts:      true,
			minFacts:         1,
			expectCorrection: true,
		},
		{
			name: "preference statement",
			messages: []map[string]any{
				{"role": "human", "content": "我偏好使用 Go 语言做后端开发。"},
				{"role": "assistant", "content": "收到"},
			},
			correction:  false,
			expectFacts: true,
			minFacts:    1,
		},
		{
			name: "question and statement mixed",
			messages: []map[string]any{
				{"role": "human", "content": "这是问题吗？但我还有一个偏好。"},
				{"role": "assistant", "content": "回答"},
			},
			correction:  false,
			expectFacts: true,
			minFacts:    1,
		},
		{
			name: "short text filtered",
			messages: []map[string]any{
				{"role": "human", "content": "短"},
				{"role": "assistant", "content": "回答"},
			},
			correction:  false,
			expectFacts: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			facts := deriveFactsFromMessages(tt.messages, tt.correction)

			if tt.expectFacts {
				if len(facts) < tt.minFacts {
					t.Errorf("expected at least %d facts, got %d", tt.minFacts, len(facts))
				}
			} else if len(facts) > 0 {
				t.Errorf("expected no facts, got %d", len(facts))
			}

			if tt.expectCorrection {
				found := false
				for _, f := range facts {
					if f.Category == CategoryCorrection {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected correction fact")
				}
			}
		})
	}
}

func TestCopyMsg(t *testing.T) {
	original := map[string]any{
		"role":    "human",
		"content": "old content",
		"extra":   "data",
	}

	copied := copyMsg(original, "new content")

	if copied["content"] != "new content" {
		t.Errorf("expected 'new content', got %v", copied["content"])
	}
	if copied["role"] != "human" {
		t.Errorf("expected 'human', got %v", copied["role"])
	}
	if copied["extra"] != "data" {
		t.Errorf("expected 'data', got %v", copied["extra"])
	}

	// Verify original is not modified
	if original["content"] != "old content" {
		t.Error("original should not be modified")
	}
}
