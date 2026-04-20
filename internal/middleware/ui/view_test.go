package ui

import (
	"context"
	"testing"

	"goclaw/internal/middleware"
)

func TestViewImageMiddleware_Name(t *testing.T) {
	mw := NewViewImageMiddleware()
	if mw.Name() != "ViewImageMiddleware" {
		t.Errorf("expected name ViewImageMiddleware, got %s", mw.Name())
	}
}

func TestViewImageMiddleware_AfterModel_NoOp(t *testing.T) {
	mw := NewViewImageMiddleware()
	state := &middleware.State{}
	if err := mw.AfterModel(context.Background(), state, nil); err != nil {
		t.Errorf("AfterModel should be no-op, got error: %v", err)
	}
}

func TestViewImageMiddleware_NoImages(t *testing.T) {
	mw := NewViewImageMiddleware()
	state := &middleware.State{
		Messages: []map[string]any{
			{"role": "human", "content": "hello"},
		},
	}

	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(state.Messages) != 1 {
		t.Error("messages should not be modified")
	}
}

func TestViewImageMiddleware_NilState(t *testing.T) {
	mw := NewViewImageMiddleware()
	if err := mw.BeforeModel(context.Background(), nil); err != nil {
		t.Errorf("expected nil error for nil state, got %v", err)
	}
}

func TestViewImageMiddleware_InjectsImages(t *testing.T) {
	mw := NewViewImageMiddleware()

	state := &middleware.State{
		Messages: []map[string]any{
			{"role": "human", "content": "Look at this image"},
			{"role": "assistant", "content": "Sure"},
		},
		ViewedImages: map[string]middleware.ViewedImage{
			"test.png": {Base64: "YWJjZGVm", MIMEType: "image/png"},
		},
	}

	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that images were injected into the last human message
	content := state.Messages[0]["content"]
	parts, ok := content.([]any)
	if !ok {
		t.Fatalf("expected content to be array, got %T", content)
	}
	if len(parts) != 2 {
		t.Errorf("expected 2 parts (text + image), got %d", len(parts))
	}

	// Check first part is text
	textPart, ok := parts[0].(map[string]any)
	if !ok {
		t.Fatalf("expected text part to be map, got %T", parts[0])
	}
	if textPart["type"] != "text" {
		t.Errorf("expected first part type=text, got %v", textPart["type"])
	}

	// Check second part is image
	imagePart, ok := parts[1].(map[string]any)
	if !ok {
		t.Fatalf("expected image part to be map, got %T", parts[1])
	}
	if imagePart["type"] != "image_url" {
		t.Errorf("expected second part type=image_url, got %v", imagePart["type"])
	}
}

func TestViewImageMiddleware_InjectsIntoCorrectMessage(t *testing.T) {
	mw := NewViewImageMiddleware()

	state := &middleware.State{
		Messages: []map[string]any{
			{"role": "human", "content": "first message"},
			{"role": "assistant", "content": "response"},
			{"role": "human", "content": "last message"},
		},
		ViewedImages: map[string]middleware.ViewedImage{
			"test.png": {Base64: "YWJj", MIMEType: "image/png"},
		},
	}

	mw.BeforeModel(context.Background(), state)

	// Check that images were injected into the LAST human message
	lastMsg := state.Messages[2]["content"]
	parts, ok := lastMsg.([]any)
	if !ok || len(parts) < 2 {
		t.Errorf("expected last human message to have image, got %v", lastMsg)
	}

	// First human message should be unchanged
	firstMsg := state.Messages[0]["content"]
	if _, ok := firstMsg.([]any); ok {
		t.Error("first human message should not have images injected")
	}
}

func TestViewImageMiddleware_SkipsEmptyBase64(t *testing.T) {
	mw := NewViewImageMiddleware()

	state := &middleware.State{
		Messages: []map[string]any{
			{"role": "human", "content": "hello"},
		},
		ViewedImages: map[string]middleware.ViewedImage{
			"empty.png": {Base64: "", MIMEType: "image/png"},
		},
	}

	mw.BeforeModel(context.Background(), state)

	// Should not inject anything
	content := state.Messages[0]["content"]
	if _, ok := content.([]any); ok {
		t.Error("expected no multimodal content for empty base64")
	}
}

func TestViewImageMiddleware_ClearsViewedImages(t *testing.T) {
	mw := NewViewImageMiddleware()

	state := &middleware.State{
		Messages: []map[string]any{
			{"role": "human", "content": "hello"},
		},
		ViewedImages: map[string]middleware.ViewedImage{
			"test.png": {Base64: "YWJj", MIMEType: "image/png"},
		},
	}

	mw.BeforeModel(context.Background(), state)

	if state.ViewedImages != nil {
		t.Errorf("expected ViewedImages to be cleared, got %v", state.ViewedImages)
	}
}

func TestViewImageMiddleware_AppendsToExistingMultimodal(t *testing.T) {
	mw := NewViewImageMiddleware()

	state := &middleware.State{
		Messages: []map[string]any{
			{
				"role": "human",
				"content": []any{
					map[string]any{"type": "text", "text": "hello"},
				},
			},
		},
		ViewedImages: map[string]middleware.ViewedImage{
			"test.png": {Base64: "YWJj", MIMEType: "image/png"},
		},
	}

	mw.BeforeModel(context.Background(), state)

	content := state.Messages[0]["content"]
	parts, ok := content.([]any)
	if !ok {
		t.Fatalf("expected array content, got %T", content)
	}
	if len(parts) != 2 {
		t.Errorf("expected 2 parts (existing + new image), got %d", len(parts))
	}
}

func TestViewImageMiddleware_MultipleImages(t *testing.T) {
	mw := NewViewImageMiddleware()

	state := &middleware.State{
		Messages: []map[string]any{
			{"role": "human", "content": "check these"},
		},
		ViewedImages: map[string]middleware.ViewedImage{
			"a.png": {Base64: "YWJj", MIMEType: "image/png"},
			"b.jpg": {Base64: "ZGVm", MIMEType: "image/jpeg"},
		},
	}

	mw.BeforeModel(context.Background(), state)

	content := state.Messages[0]["content"]
	parts, ok := content.([]any)
	if !ok {
		t.Fatalf("expected array content, got %T", content)
	}
	// 1 text + 2 images = 3 parts
	if len(parts) != 3 {
		t.Errorf("expected 3 parts, got %d", len(parts))
	}
}

func TestGuessMIMEType(t *testing.T) {
	tests := []struct {
		path, want string
	}{
		{"test.png", "image/png"},
		{"test.PNG", "image/png"},
		{"test.jpg", "image/jpeg"},
		{"test.jpeg", "image/jpeg"},
		{"test.JPG", "image/jpeg"},
		{"test.gif", "image/gif"},
		{"test.webp", "image/webp"},
		{"test.svg", "image/svg+xml"},
		{"test.unknown", "image/png"}, // default
		{"", "image/png"},              // default
	}

	for _, tt := range tests {
		got := guessMIMEType(tt.path)
		if got != tt.want {
			t.Errorf("guessMIMEType(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestIsValidBase64Image(t *testing.T) {
	tests := []struct {
		data  string
		valid bool
	}{
		{"", false},
		{"aGVsbG8=", true},      // valid base64
		{"YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXo=", true}, // valid
		{"!invalid", false},    // invalid chars
		{"YWJj ZGVm", false},    // space not valid
	}

	for _, tt := range tests {
		got := IsValidBase64Image(tt.data)
		if got != tt.valid {
			t.Errorf("IsValidBase64Image(%q) = %v, want %v", tt.data, got, tt.valid)
		}
	}
}
