package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// writeSSE serialises event as an SSE frame with optional event ID and flushes it.
// Format:
//
//	id: <event_id>\n (if EventID is set)
//	data: <json>\n\n
//
// The caller must have already set the response headers for text/event-stream.
func writeSSE(w io.Writer, event SSEEvent) error {
	// Write event ID if present (for SSE resume support)
	if event.EventID != "" {
		if _, err := fmt.Fprintf(w, "id: %s\n", event.EventID); err != nil {
			return fmt.Errorf("writeSSE id: %w", err)
		}
	}
	b, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("writeSSE marshal: %w", err)
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
		return fmt.Errorf("writeSSE write: %w", err)
	}
	return nil
}

// sseNow returns the current UTC time as Unix milliseconds.
func sseNow() int64 {
	return time.Now().UnixMilli()
}
