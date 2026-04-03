package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSuggestionsHandler_GenerateSuggestions_Fallback(t *testing.T) {
	h := NewSuggestionsHandler(nil)
	body := `{"messages":[],"count":2}`
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

func TestParseSuggestions(t *testing.T) {
	content := "1. What is the next step?\n2. Can you clarify?\n3. Show an example."
	got := parseSuggestions(content, 3)
	if len(got) != 3 {
		t.Errorf("expected 3, got %d", len(got))
	}
	if got[0] != "What is the next step?" {
		t.Errorf("unexpected first suggestion: %q", got[0])
	}
}
