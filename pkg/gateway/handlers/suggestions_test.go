package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goclaw/internal/config"
)

func TestSuggestionsHandler_GenerateSuggestions_Fallback(t *testing.T) {
	h := NewSuggestionsHandler(nil)
	body := `{"messages":[{"role":"user","content":"hi"}],"count":2}`
	req := httptest.NewRequest(http.MethodPost, "/api/threads/t1/suggestions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"thread_id": "t1"})

	h.GenerateSuggestions(ctx)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	suggestions := resp["suggestions"].([]any)
	if len(suggestions) != 2 {
		t.Errorf("expected 2 suggestions, got %d", len(suggestions))
	}
}

func TestParseSuggestions_JSONArray(t *testing.T) {
	content := `["First question?", "Second question?", "Third question?"]`
	got := parseSuggestions(content, 2)
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	if got[0] != "First question?" {
		t.Fatalf("unexpected first suggestion: %q", got[0])
	}
}

func TestParseSuggestions_JSONWithTrailingGarbage(t *testing.T) {
	content := `["A", "B"] extra text`
	got := parseSuggestions(content, 3)
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d (%v)", len(got), got)
	}
}

func TestParseSuggestions_NumberedListFallback(t *testing.T) {
	content := "1. What is the next step?\n2. Can you clarify?\n3. Show an example."
	got := parseSuggestions(content, 3)
	if len(got) != 3 {
		t.Errorf("expected 3, got %d", len(got))
	}
	if got[0] != "What is the next step?" {
		t.Errorf("unexpected first suggestion: %q", got[0])
	}
}

func TestGenerateSuggestions_EmptyMessages(t *testing.T) {
	h := NewSuggestionsHandler(nil)
	body := `{"messages": [], "count": 3}`
	req := httptest.NewRequest(http.MethodPost, "/api/threads/t1/suggestions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"thread_id": "t1"})

	h.GenerateSuggestions(ctx)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestGenerateSuggestions_InvalidJSON(t *testing.T) {
	h := NewSuggestionsHandler(nil)
	body := `invalid json`
	req := httptest.NewRequest(http.MethodPost, "/api/threads/t1/suggestions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"thread_id": "t1"})

	h.GenerateSuggestions(ctx)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestGenerateSuggestions_WithConfig(t *testing.T) {
	h := NewSuggestionsHandler(&config.AppConfig{Models: []config.ModelConfig{}})
	body := `{"messages": [{"role": "user", "content": "hello"}], "count": 2}`
	req := httptest.NewRequest(http.MethodPost, "/api/threads/t1/suggestions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"thread_id": "t1"})

	h.GenerateSuggestions(ctx)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestFallbackSuggestions(t *testing.T) {
	got := fallbackSuggestions(3)
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
}

func TestFallbackSuggestions_ExceedMax(t *testing.T) {
	got := fallbackSuggestions(10)
	if len(got) != 5 {
		t.Fatalf("expected 5 (max available), got %d", len(got))
	}
}

func TestBuildSuggestionPrompt(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "Hello"},
		{"role": "assistant", "content": "Hi there!"},
	}
	prompt := buildSuggestionPrompt(messages, 3)
	if len(prompt) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(prompt))
	}
}

func TestBuildSuggestionPrompt_EmptyMessages(t *testing.T) {
	messages := []map[string]any{}
	prompt := buildSuggestionPrompt(messages, 3)
	if len(prompt) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(prompt))
	}
}

func TestParseJSONStringList_Empty(t *testing.T) {
	got := parseJSONStringList("")
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestParseJSONStringList_Fenced(t *testing.T) {
	content := "```json\n[\"a\", \"b\"]\n```"
	got := parseJSONStringList(content)
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
}

func TestParseJSONStringList_AnyArray(t *testing.T) {
	content := `[1, 2, 3]`
	got := parseJSONStringList(content)
	// Non-string elements should be filtered out
	if len(got) != 0 {
		t.Fatalf("expected 0 (non-strings filtered), got %d", len(got))
	}
}

func TestCompactNonEmpty(t *testing.T) {
	in := []string{"  a  ", "", "b", "  ", "c"}
	got := compactNonEmpty(in)
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
}

func TestSplitLines(t *testing.T) {
	s := "line1\nline2\nline3"
	got := splitLines(s)
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
}

func TestSplitLines_Empty(t *testing.T) {
	got := splitLines("")
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestTrimListPrefix(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"1. Hello", "Hello"},
		{"2) World", "World"},
		{"- Item", "Item"},
		{"No prefix", "No prefix"},
	}
	for _, tc := range cases {
		got := trimListPrefix(tc.in)
		if got != tc.want {
			t.Errorf("trimListPrefix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
