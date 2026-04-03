package tools

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strconv"
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

func TestReadMCPFrame(t *testing.T) {
	payload := `{"jsonrpc":"2.0","id":1,"result":{}}`
	frame := "Content-Length: " + strconv.Itoa(len(payload)) + "\r\n\r\n" + payload
	got, err := readMCPFrame(bufio.NewReader(strings.NewReader(frame)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("payload mismatch: %q", string(got))
	}
}

func TestMCPFramedClientWrite(t *testing.T) {
	var buf bytes.Buffer
	c := &mcpFramedClient{writer: &buf}
	id := 1
	msg := mcpEnvelope{JSONRPC: "2.0", ID: &id, Method: "ping"}
	if err := c.write(msg); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Content-Length:") || !strings.Contains(buf.String(), `"method":"ping"`) {
		t.Fatalf("unexpected frame: %s", buf.String())
	}
}
