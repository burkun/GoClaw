package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/bookerbai/goclaw/internal/config"
)

const defaultMCPStdioTimeout = 30 * time.Second

type mcpEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *mcpRPCError    `json:"error,omitempty"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpToolListResult struct {
	Tools []struct {
		Name string `json:"name"`
	} `json:"tools"`
}

type mcpToolCallResult struct {
	IsError bool `json:"isError"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"content"`
}

type mcpFramedClient struct {
	reader *bufio.Reader
	writer io.Writer
	nextID int
}

func invokeMCPStdio(ctx context.Context, serverName string, serverCfg config.MCPServerConfig, in mcpToolInput) (string, error) {
	command := strings.TrimSpace(serverCfg.Command)
	if command == "" {
		return "", fmt.Errorf("stdio server %q requires command", serverName)
	}

	runCtx := ctx
	if _, ok := runCtx.Deadline(); !ok {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, defaultMCPStdioTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, command, serverCfg.Args...)
	cmd.Env = mergeMCPEnv(serverCfg.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("open stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("open stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start stdio server %q: %w", serverName, err)
	}
	defer terminateMCPProcess(cmd, stdin)

	client := &mcpFramedClient{reader: bufio.NewReader(stdout), writer: stdin}

	if _, err := client.request(runCtx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "goclaw",
			"version": "0.1.0",
		},
	}); err != nil {
		return "", fmt.Errorf("initialize failed: %w; stderr=%s", err, strings.TrimSpace(stderr.String()))
	}

	if err := client.notify("notifications/initialized", map[string]any{}); err != nil {
		return "", fmt.Errorf("initialized notify failed: %w", err)
	}

	listRaw, err := client.request(runCtx, "tools/list", map[string]any{})
	if err != nil {
		return "", fmt.Errorf("tools/list failed: %w", err)
	}
	if err := ensureMCPToolExposed(listRaw, in.ToolName); err != nil {
		return "", err
	}

	callRaw, err := client.request(runCtx, "tools/call", map[string]any{
		"name":      in.ToolName,
		"arguments": defaultMCPArguments(in.Arguments),
	})
	if err != nil {
		return "", fmt.Errorf("tools/call failed: %w", err)
	}

	return formatMCPCallResult(callRaw)
}

func (c *mcpFramedClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.nextID++
	id := c.nextID
	idCopy := id
	msg := mcpEnvelope{
		JSONRPC: "2.0",
		ID:      &idCopy,
		Method:  method,
	}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		msg.Params = b
	}

	if err := c.write(msg); err != nil {
		return nil, err
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		env, err := c.read()
		if err != nil {
			return nil, err
		}
		if env.ID == nil || *env.ID != id {
			continue
		}
		if env.Error != nil {
			return nil, fmt.Errorf("rpc error code=%d message=%s", env.Error.Code, env.Error.Message)
		}
		if len(env.Result) == 0 {
			return json.RawMessage("{}"), nil
		}
		return env.Result, nil
	}
}

func (c *mcpFramedClient) notify(method string, params any) error {
	msg := mcpEnvelope{JSONRPC: "2.0", Method: method}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal params: %w", err)
		}
		msg.Params = b
	}
	return c.write(msg)
}

func (c *mcpFramedClient) write(msg mcpEnvelope) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	if _, err := fmt.Fprintf(c.writer, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := c.writer.Write(payload); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}
	return nil
}

func (c *mcpFramedClient) read() (mcpEnvelope, error) {
	payload, err := readMCPFrame(c.reader)
	if err != nil {
		return mcpEnvelope{}, err
	}
	var env mcpEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return mcpEnvelope{}, fmt.Errorf("decode response: %w", err)
	}
	return env, nil
}

func readMCPFrame(r *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read frame header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(parts[0]), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil || n < 0 {
				return nil, fmt.Errorf("invalid content-length %q", strings.TrimSpace(parts[1]))
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("read frame payload: %w", err)
	}
	return payload, nil
}

func ensureMCPToolExposed(listRaw json.RawMessage, toolName string) error {
	var list mcpToolListResult
	if err := json.Unmarshal(listRaw, &list); err != nil {
		return fmt.Errorf("decode tools/list result: %w", err)
	}
	for _, t := range list.Tools {
		if t.Name == toolName {
			return nil
		}
	}
	return fmt.Errorf("tool %q not found in MCP server tool list", toolName)
}

func formatMCPCallResult(callRaw json.RawMessage) (string, error) {
	var res mcpToolCallResult
	if err := json.Unmarshal(callRaw, &res); err != nil {
		return string(callRaw), nil
	}
	if res.IsError {
		if len(res.Content) > 0 && strings.TrimSpace(res.Content[0].Text) != "" {
			return "", fmt.Errorf("mcp tool returned error: %s", strings.TrimSpace(res.Content[0].Text))
		}
		return "", fmt.Errorf("mcp tool returned error")
	}
	if len(res.Content) == 1 && res.Content[0].Type == "text" {
		return res.Content[0].Text, nil
	}
	return string(callRaw), nil
}

func defaultMCPArguments(v map[string]any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	return v
}

func mergeMCPEnv(extra map[string]string) []string {
	base := append([]string{}, os.Environ()...)
	for k, v := range extra {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		base = append(base, key+"="+v)
	}
	return base
}

func terminateMCPProcess(cmd *exec.Cmd, stdin io.Closer) {
	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-done
	}
}
