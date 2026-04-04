// Package viewimage implements ViewImageMiddleware for GoClaw.
//
// ViewImageMiddleware injects base64-encoded images from state.ViewedImages
// into the message list as multimodal content before model inference.
package viewimage

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// ViewImageMiddleware handles image injection for multimodal models.
type ViewImageMiddleware struct {
	middleware.MiddlewareWrapper
}

// NewViewImageMiddleware constructs a ViewImageMiddleware.
func NewViewImageMiddleware() *ViewImageMiddleware {
	return &ViewImageMiddleware{}
}

// Name implements middleware.Middleware.
func (m *ViewImageMiddleware) Name() string { return "ViewImageMiddleware" }

// Before injects viewed images into the message list.
func (m *ViewImageMiddleware) Before(_ context.Context, state *middleware.State) error {
	if state == nil || len(state.ViewedImages) == 0 {
		return nil
	}

	// Build multimodal content parts for each image.
	var imageParts []map[string]any
	for path, imgData := range state.ViewedImages {
		if imgData.Base64 == "" {
			continue
		}

		mimeType := imgData.MIMEType
		if mimeType == "" {
			mimeType = guessMIMEType(path)
		}

		imageParts = append(imageParts, map[string]any{
			"type": "image_url",
			"image_url": map[string]string{
				"url": fmt.Sprintf("data:%s;base64,%s", mimeType, imgData.Base64),
			},
		})
	}

	if len(imageParts) == 0 {
		return nil
	}

	// Find the last human message and inject images.
	for i := len(state.Messages) - 1; i >= 0; i-- {
		msg := state.Messages[i]
		role, _ := msg["role"].(string)
		if role != "human" && role != "user" {
			continue
		}

		// Check if content is already multimodal.
		if existing, ok := msg["content"].([]any); ok {
			for _, part := range imageParts {
				existing = append(existing, part)
			}
			msg["content"] = existing
		} else if textContent, ok := msg["content"].(string); ok {
			// Convert text content to multimodal array.
			parts := []any{
				map[string]any{"type": "text", "text": textContent},
			}
			for _, part := range imageParts {
				parts = append(parts, part)
			}
			msg["content"] = parts
		}

		state.Messages[i] = msg
		break
	}

	// Clear viewed images after injection.
	state.ViewedImages = nil

	return nil
}

// After is a no-op.
func (m *ViewImageMiddleware) After(_ context.Context, _ *middleware.State, _ *middleware.Response) error {
	return nil
}

func guessMIMEType(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	case strings.HasSuffix(lower, ".svg"):
		return "image/svg+xml"
	default:
		return "image/png"
	}
}

// IsValidBase64Image checks if the string is valid base64 image data.
func IsValidBase64Image(data string) bool {
	if data == "" {
		return false
	}
	_, err := base64.StdEncoding.DecodeString(data)
	return err == nil
}

var _ middleware.Middleware = (*ViewImageMiddleware)(nil)
