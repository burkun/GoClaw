package memory

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMemoryUpdate_HasUpdates(t *testing.T) {
	tests := []struct {
		name     string
		update   MemoryUpdate
		expected bool
	}{
		{
			name:     "empty update",
			update:   MemoryUpdate{},
			expected: false,
		},
		{
			name: "user work context update",
			update: MemoryUpdate{
				User: UserUpdateSections{
					WorkContext: SectionUpdate{ShouldUpdate: true},
				},
			},
			expected: true,
		},
		{
			name: "user personal context update",
			update: MemoryUpdate{
				User: UserUpdateSections{
					PersonalContext: SectionUpdate{ShouldUpdate: true},
				},
			},
			expected: true,
		},
		{
			name: "user top of mind update",
			update: MemoryUpdate{
				User: UserUpdateSections{
					TopOfMind: SectionUpdate{ShouldUpdate: true},
				},
			},
			expected: true,
		},
		{
			name: "history recent months update",
			update: MemoryUpdate{
				History: HistoryUpdateSections{
					RecentMonths: SectionUpdate{ShouldUpdate: true},
				},
			},
			expected: true,
		},
		{
			name: "history earlier context update",
			update: MemoryUpdate{
				History: HistoryUpdateSections{
					EarlierContext: SectionUpdate{ShouldUpdate: true},
				},
			},
			expected: true,
		},
		{
			name: "history long term background update",
			update: MemoryUpdate{
				History: HistoryUpdateSections{
					LongTermBackground: SectionUpdate{ShouldUpdate: true},
				},
			},
			expected: true,
		},
		{
			name: "new facts",
			update: MemoryUpdate{
				NewFacts: []NewFact{{Content: "test"}},
			},
			expected: true,
		},
		{
			name: "facts to remove",
			update: MemoryUpdate{
				FactsToRemove: []string{"fact_1"},
			},
			expected: true,
		},
		{
			name: "all updates",
			update: MemoryUpdate{
				User: UserUpdateSections{
					WorkContext: SectionUpdate{ShouldUpdate: true},
				},
				NewFacts:      []NewFact{{Content: "test"}},
				FactsToRemove: []string{"fact_1"},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.update.HasUpdates()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestApplyUpdates_Nil(t *testing.T) {
	result := ApplyUpdates(nil, &MemoryUpdate{}, "thread-1")
	assert.False(t, result)

	result = ApplyUpdates(&Memory{}, nil, "thread-1")
	assert.False(t, result)
}

func TestApplyUpdates_UserContext(t *testing.T) {
	mem := newEmptyMemory()
	update := &MemoryUpdate{
		User: UserUpdateSections{
			WorkContext: SectionUpdate{
				Summary:      "Works at Acme Corp",
				ShouldUpdate: true,
			},
			PersonalContext: SectionUpdate{
				Summary:      "Lives in NYC",
				ShouldUpdate: true,
			},
			TopOfMind: SectionUpdate{
				Summary:      "Working on GoClaw project",
				ShouldUpdate: true,
			},
		},
	}

	result := ApplyUpdates(mem, update, "thread-1")
	assert.True(t, result)
	assert.Equal(t, "Works at Acme Corp", mem.User.WorkContext.Summary)
	assert.Equal(t, "Lives in NYC", mem.User.PersonalContext.Summary)
	assert.Equal(t, "Working on GoClaw project", mem.User.TopOfMind.Summary)
}

func TestApplyUpdates_HistoryContext(t *testing.T) {
	mem := newEmptyMemory()
	update := &MemoryUpdate{
		History: HistoryUpdateSections{
			RecentMonths: SectionUpdate{
				Summary:      "Recent work summary",
				ShouldUpdate: true,
			},
			EarlierContext: SectionUpdate{
				Summary:      "Earlier context",
				ShouldUpdate: true,
			},
			LongTermBackground: SectionUpdate{
				Summary:      "Long term background",
				ShouldUpdate: true,
			},
		},
	}

	result := ApplyUpdates(mem, update, "thread-1")
	assert.True(t, result)
	assert.Equal(t, "Recent work summary", mem.History.RecentMonths.Summary)
	assert.Equal(t, "Earlier context", mem.History.EarlierContext.Summary)
	assert.Equal(t, "Long term background", mem.History.LongTermBackground.Summary)
}

func TestApplyUpdates_NewFacts(t *testing.T) {
	mem := newEmptyMemory()
	update := &MemoryUpdate{
		NewFacts: []NewFact{
			{Content: "User prefers Go", Category: "preference", Confidence: 0.9},
			{Content: "User works at Acme", Category: "context", Confidence: 0.8},
		},
	}

	result := ApplyUpdates(mem, update, "thread-1")
	assert.True(t, result)
	assert.Len(t, mem.Facts, 2)
	assert.Equal(t, "User prefers Go", mem.Facts[0].Content)
	assert.Equal(t, "thread-1", mem.Facts[0].Source)
}

func TestApplyUpdates_NewFacts_LowConfidence(t *testing.T) {
	mem := newEmptyMemory()
	update := &MemoryUpdate{
		NewFacts: []NewFact{
			{Content: "Low confidence fact", Category: "context", Confidence: 0.5},
		},
	}

	result := ApplyUpdates(mem, update, "thread-1")
	assert.False(t, result)
	assert.Len(t, mem.Facts, 0)
}

func TestApplyUpdates_NewFacts_Duplicate(t *testing.T) {
	mem := &Memory{
		Facts: []MemoryFact{{Content: "User prefers Go"}},
	}
	update := &MemoryUpdate{
		NewFacts: []NewFact{
			{Content: "User prefers Go", Category: "preference", Confidence: 0.9},
		},
	}

	result := ApplyUpdates(mem, update, "thread-1")
	assert.False(t, result)
	assert.Len(t, mem.Facts, 1)
}

func TestApplyUpdates_FactsToRemove(t *testing.T) {
	mem := &Memory{
		Facts: []MemoryFact{
			{ID: "fact_1", Content: "Fact 1"},
			{ID: "fact_2", Content: "Fact 2"},
			{ID: "fact_3", Content: "Fact 3"},
		},
	}
	update := &MemoryUpdate{
		FactsToRemove: []string{"fact_1", "fact_3"},
	}

	result := ApplyUpdates(mem, update, "thread-1")
	assert.True(t, result)
	assert.Len(t, mem.Facts, 1)
	assert.Equal(t, "fact_2", mem.Facts[0].ID)
}

func TestApplyUpdates_EmptySummaryNotApplied(t *testing.T) {
	mem := newEmptyMemory()
	update := &MemoryUpdate{
		User: UserUpdateSections{
			WorkContext: SectionUpdate{
				Summary:      "",
				ShouldUpdate: true,
			},
		},
	}

	result := ApplyUpdates(mem, update, "thread-1")
	assert.False(t, result)
}

func TestParseMemoryUpdate_Empty(t *testing.T) {
	result, err := parseMemoryUpdate("", 0.7)
	assert.NoError(t, err)
	assert.Nil(t, result)

	result, err = parseMemoryUpdate("   ", 0.7)
	assert.NoError(t, err)
	assert.Nil(t, result)

	result, err = parseMemoryUpdate("{}", 0.7)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestParseMemoryUpdate_ValidJSON(t *testing.T) {
	raw := `{
		"user": {
			"workContext": { "summary": "test", "shouldUpdate": true }
		},
		"newFacts": [
			{ "content": "fact1", "category": "preference", "confidence": 0.9 }
		]
	}`

	result, err := parseMemoryUpdate(raw, 0.7)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.User.WorkContext.ShouldUpdate)
	assert.Len(t, result.NewFacts, 1)
}

func TestParseMemoryUpdate_MarkdownCodeFence(t *testing.T) {
	raw := "```json\n{\"user\": {\"workContext\": {\"summary\": \"test\", \"shouldUpdate\": true}}}\n```"

	result, err := parseMemoryUpdate(raw, 0.7)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestParseMemoryUpdate_ExtractJSON(t *testing.T) {
	raw := "Some text before {\"newFacts\": []} some text after"

	result, err := parseMemoryUpdate(raw, 0.7)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestParseMemoryUpdate_ConfidenceFilter(t *testing.T) {
	raw := `{
		"newFacts": [
			{ "content": "high", "category": "preference", "confidence": 0.9 },
			{ "content": "low", "category": "preference", "confidence": 0.5 }
		]
	}`

	result, err := parseMemoryUpdate(raw, 0.7)
	assert.NoError(t, err)
	assert.Len(t, result.NewFacts, 1)
	assert.Equal(t, "high", result.NewFacts[0].Content)
}

func TestParseMemoryUpdate_DefaultConfidence(t *testing.T) {
	raw := `{
		"newFacts": [
			{ "content": "no confidence", "category": "preference" }
		]
	}`

	result, err := parseMemoryUpdate(raw, 0.7)
	assert.NoError(t, err)
	assert.Len(t, result.NewFacts, 1)
	assert.Equal(t, 0.8, result.NewFacts[0].Confidence)
}

func TestStripUploadMentionsFromMemory(t *testing.T) {
	mem := &Memory{
		User: UserContext{
			WorkContext: ContextSection{
				Summary: "User uploaded a file. Works at Acme.",
			},
		},
		Facts: []MemoryFact{
			{Content: "User uploaded file test.pdf"},
			{Content: "User prefers Go"},
		},
	}

	stripUploadMentionsFromMemory(mem)

	assert.Contains(t, mem.User.WorkContext.Summary, "Acme")
	assert.NotContains(t, mem.User.WorkContext.Summary, "uploaded")
	assert.Len(t, mem.Facts, 1)
	assert.Equal(t, "User prefers Go", mem.Facts[0].Content)
}

func TestCleanUploadMentions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{
			name:     "no upload mentions",
			input:    "User works at Acme Corp.",
			contains: "Acme",
		},
		{
			name:     "upload mention cleaned",
			input:    "User uploaded file test.pdf. User works at Acme.",
			contains: "Acme",
		},
		{
			name:     "empty string",
			input:    "",
			contains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanUploadMentions(tt.input)
			if tt.contains != "" && !contains(result, tt.contains) {
				t.Errorf("expected result to contain %q, got %q", tt.contains, result)
			}
		})
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestGenerateFactID(t *testing.T) {
	id1 := generateFactID()
	id2 := generateFactID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.Contains(t, id1, "fact_")
	assert.Contains(t, id2, "fact_")
}

func TestFormatConversationForUpdate(t *testing.T) {
	messages := []map[string]any{
		{"role": "human", "content": "Hello"},
		{"role": "assistant", "content": "Hi there!"},
		{"role": "tool", "content": "tool result"},
		{"role": "human", "content": "<uploaded_files>file content</uploaded_files>Real message"},
	}

	result := formatConversationForUpdate(messages)

	assert.Contains(t, result, "User: Hello")
	assert.Contains(t, result, "Assistant: Hi there!")
	assert.NotContains(t, result, "tool result")
	assert.NotContains(t, result, "<uploaded_files>")
	assert.Contains(t, result, "Real message")
}

func TestFormatConversationForUpdate_TruncateLongMessage(t *testing.T) {
	longContent := string(make([]byte, 1500))
	for i := range longContent {
		longContent = longContent[:i] + "x" + longContent[i+1:]
	}

	messages := []map[string]any{
		{"role": "human", "content": longContent},
	}

	result := formatConversationForUpdate(messages)

	assert.LessOrEqual(t, len(result), 1100) // 1000 + "User: " + "..."
	assert.Contains(t, result, "...")
}

func TestFormatConversationForUpdate_EmptyContent(t *testing.T) {
	messages := []map[string]any{
		{"role": "human", "content": ""},
		{"role": "assistant", "content": "   "},
	}

	result := formatConversationForUpdate(messages)
	assert.Empty(t, result)
}
