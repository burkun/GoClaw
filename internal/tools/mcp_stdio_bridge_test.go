package tools

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestEnsureMCPToolExposed(t *testing.T) {
	list := json.RawMessage(`{"tools":[{"name":"a"},{"name":"b"}]}`)
	if err := ensureMCPToolExposed(list, "b"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ensureMCPToolExposed(list, "c"); err == nil {
		t.Fatalf("expected not found error")
	}
}

func TestFormatMCPCallResult(t *testing.T) {
	out, err := formatMCPCallResult(json.RawMessage(`{"isError":false,"content":[{"type":"text","text":"hello"}]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello" {
		t.Fatalf("unexpected output: %q", out)
	}

	_, err = formatMCPCallResult(json.RawMessage(`{"isError":true,"content":[{"type":"text","text":"denied"}]}`))
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("expected tool error, got %v", err)
	}
}

func TestReadMCPContentLengthFrame(t *testing.T) {
	payload := `{"jsonrpc":"2.0","id":1,"result":{}}`
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(payload), payload)
	got, err := readMCPContentLengthFrame(bufio.NewReader(strings.NewReader(frame)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("payload mismatch: %q", string(got))
	}
}

func TestReadMCPLineFrame(t *testing.T) {
	payload := `{"jsonrpc":"2.0","id":1,"result":{}}`
	frame := payload + "\n"
	got, err := readMCPLineFrame(bufio.NewReader(strings.NewReader(frame)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("payload mismatch: %q", string(got))
	}
}

func TestMCPFramedClientWrite_ContentLength(t *testing.T) {
	var buf bytes.Buffer
	c := &mcpFramedClient{writer: &buf, lineFramed: false}
	id := 1
	msg := mcpEnvelope{JSONRPC: "2.0", ID: &id, Method: "ping"}
	if err := c.write(msg); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Content-Length:") || !strings.Contains(buf.String(), `"method":"ping"`) {
		t.Fatalf("unexpected frame: %s", buf.String())
	}
}

func TestMCPFramedClientWrite_LineFramed(t *testing.T) {
	var buf bytes.Buffer
	c := &mcpFramedClient{writer: &buf, lineFramed: true}
	id := 1
	msg := mcpEnvelope{JSONRPC: "2.0", ID: &id, Method: "ping"}
	if err := c.write(msg); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	// Line-framed should NOT have Content-Length header
	if strings.Contains(buf.String(), "Content-Length:") {
		t.Fatalf("unexpected Content-Length in line-framed mode: %s", buf.String())
	}
	if !strings.Contains(buf.String(), `"method":"ping"`) || !strings.HasSuffix(buf.String(), "\n") {
		t.Fatalf("unexpected frame: %s", buf.String())
	}
}
