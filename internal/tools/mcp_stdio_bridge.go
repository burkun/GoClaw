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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"goclaw/internal/config"
	"goclaw/internal/logging"
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
	reader     *bufio.Reader
	writer     io.Writer
	nextID     int
	lineFramed bool // true = server uses newline-delimited JSON; false = Content-Length framing
}

// detectFrameFormat peeks at the first byte to determine the server's framing format.
// If it starts with '{', the server uses newline-delimited JSON.
// If it starts with 'C' (Content-Length), the server uses header-based framing.
func detectFrameFormat(r *bufio.Reader) (lineFramed bool, err error) {
	b, err := r.Peek(1)
	if err != nil {
		return false, fmt.Errorf("peek frame format: %w", err)
	}
	return b[0] == '{', nil
}

type pooledStdioClient struct {
	mu          sync.Mutex
	serverName  string
	serverCfg   config.MCPServerConfig
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      io.ReadCloser
	stderr      bytes.Buffer
	client      *mcpFramedClient
	initialized bool
}

type stdioClientPool struct {
	mu      sync.Mutex
	clients map[string]*pooledStdioClient
}

var globalStdioClientPool = &stdioClientPool{clients: map[string]*pooledStdioClient{}}

func (p *stdioClientPool) get(serverName string, serverCfg config.MCPServerConfig) *pooledStdioClient {
	key := stdioPoolKey(serverName, serverCfg)
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.clients[key]; ok {
		return c
	}
	c := &pooledStdioClient{serverName: serverName, serverCfg: serverCfg}
	p.clients[key] = c
	return c
}

// invalidate stops and removes clients matching the given serverName,
// forcing reconnection on next use.
func (p *stdioClientPool) invalidate(serverName string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	keysToRemove := make([]string, 0)
	for key, client := range p.clients {
		if client.serverName == serverName {
			client.mu.Lock()
			client.stopLocked()
			client.mu.Unlock()
			keysToRemove = append(keysToRemove, key)
		}
	}
	for _, key := range keysToRemove {
		delete(p.clients, key)
	}
}

func stdioPoolKey(serverName string, serverCfg config.MCPServerConfig) string {
	parts := []string{serverName, strings.TrimSpace(serverCfg.Command), strings.Join(serverCfg.Args, "\x00")}
	if len(serverCfg.Env) > 0 {
		keys := make([]string, 0, len(serverCfg.Env))
		for k := range serverCfg.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			parts = append(parts, k+"="+serverCfg.Env[k])
		}
	}
	return strings.Join(parts, "\x1f")
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

	if hasMCPConfigChanged(serverName, serverCfg) {
		// Config changed; invalidate and recreate pool with new key based on command+args+env.
		globalStdioClientPool.invalidate(serverName)
	}

	client := globalStdioClientPool.get(serverName, serverCfg)
	return client.invoke(runCtx, in)
}

func (p *pooledStdioClient) invoke(ctx context.Context, in mcpToolInput) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.ensureStarted(); err != nil {
		return "", err
	}

	if !p.initialized {
		if _, err := p.client.request(ctx, "initialize", map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "goclaw",
				"version": "0.1.0",
			},
		}); err != nil {
			p.stopLocked()
			return "", fmt.Errorf("initialize failed: %w; stderr=%s", err, strings.TrimSpace(p.stderr.String()))
		}
		if err := p.client.notify("notifications/initialized", map[string]any{}); err != nil {
			p.stopLocked()
			return "", fmt.Errorf("initialized notify failed: %w", err)
		}
		p.initialized = true
	}

	listRaw, err := p.client.request(ctx, "tools/list", map[string]any{})
	if err != nil {
		p.stopLocked()
		return "", fmt.Errorf("tools/list failed: %w", err)
	}
	if err := ensureMCPToolExposed(listRaw, in.ToolName); err != nil {
		return "", err
	}

	callRaw, err := p.client.request(ctx, "tools/call", map[string]any{
		"name":      in.ToolName,
		"arguments": defaultMCPArguments(in.Arguments),
	})
	if err != nil {
		p.stopLocked()
		return "", fmt.Errorf("tools/call failed: %w", err)
	}

	return formatMCPCallResult(callRaw)
}

func (p *pooledStdioClient) ensureStarted() error {
	if p.cmd != nil && p.cmd.Process != nil {
		if p.cmd.ProcessState == nil {
			return nil
		}
	}

	p.stderr.Reset()
	cmd := exec.Command(strings.TrimSpace(p.serverCfg.Command), p.serverCfg.Args...)
	cmd.Env = mergeMCPEnv(p.serverCfg.Env)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("open stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open stdout pipe: %w", err)
	}
	cmd.Stderr = &p.stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start stdio server %q: %w", p.serverName, err)
	}

	// Give the process a moment to initialize.
	// Some MCP servers need time to set up before they can process input.
	time.Sleep(100 * time.Millisecond)

	reader := bufio.NewReader(stdout)
	p.cmd = cmd
	p.stdin = stdin
	p.stdout = stdout
	// Default to newline-delimited JSON format for better compatibility.
	// Many MCP servers (including @modelcontextprotocol/server-filesystem)
	// use newline-delimited JSON instead of Content-Length framing.
	p.client = &mcpFramedClient{reader: reader, writer: stdin, lineFramed: true}
	p.initialized = false
	return nil
}

func (p *pooledStdioClient) stopLocked() {
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	if p.stdout != nil {
		_ = p.stdout.Close()
	}
	if p.cmd != nil {
		terminateMCPProcess(p.cmd, nil)
	}
	p.cmd = nil
	p.stdin = nil
	p.stdout = nil
	p.client = nil
	p.initialized = false
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
	// Check if params is nil or a nil map/slice.
	// In Go, a nil map passed to any interface is NOT equal to nil.
	// We need to marshal and check for "null" to handle this correctly.
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		// Only set params if it's not "null" (which happens with nil maps/slices)
		if string(b) != "null" {
			msg.Params = b
		}
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
	if c.lineFramed {
		// Newline-delimited JSON: write JSON + newline
		n, err := fmt.Fprintf(c.writer, "%s\n", payload)
		if err != nil {
			return fmt.Errorf("write line frame: %w", err)
		}
		// Flush the writer to ensure the message is sent immediately.
		// This is critical for stdin pipes which may be buffered.
		if flusher, ok := c.writer.(interface{ Flush() error }); ok {
			if err := flusher.Flush(); err != nil {
				return fmt.Errorf("flush writer: %w", err)
			}
		}
		logging.Debug("[MCP stdio] wrote line frame", "bytes", n, "payload", string(payload))
	} else {
		// Content-Length framing (LSP-style)
		if _, err := fmt.Fprintf(c.writer, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
			return fmt.Errorf("write header: %w", err)
		}
		if _, err := c.writer.Write(payload); err != nil {
			return fmt.Errorf("write payload: %w", err)
		}
		// Flush for buffered writers.
		if flusher, ok := c.writer.(interface{ Flush() error }); ok {
			if err := flusher.Flush(); err != nil {
				return fmt.Errorf("flush writer: %w", err)
			}
		}
	}
	return nil
}

func (c *mcpFramedClient) read() (mcpEnvelope, error) {
	var payload []byte
	var err error

	if c.lineFramed {
		payload, err = readMCPLineFrame(c.reader)
	} else {
		payload, err = readMCPContentLengthFrame(c.reader)
	}
	if err != nil {
		return mcpEnvelope{}, err
	}

	var env mcpEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return mcpEnvelope{}, fmt.Errorf("decode response: %w", err)
	}
	return env, nil
}

// readMCPLineFrame reads a newline-delimited JSON frame.
// Many MCP servers use this simpler format instead of Content-Length framing.
func readMCPLineFrame(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read line frame: %w", err)
	}
	return bytes.TrimRight(line, "\r\n"), nil
}

// readMCPContentLengthFrame reads a Content-Length framed message (LSP-style).
// This is used by spec-compliant MCP servers that implement the full framing protocol.
func readMCPContentLengthFrame(r *bufio.Reader) ([]byte, error) {
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

// invokeMCPStdioRaw invokes an arbitrary MCP method via STDIO and returns the raw result.
// This is used for tool discovery (tools/list) before creating specific tool wrappers.
func invokeMCPStdioRaw(ctx context.Context, serverName string, serverCfg config.MCPServerConfig, method string, params map[string]any) (string, error) {
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

	if hasMCPConfigChanged(serverName, serverCfg) {
		globalStdioClientPool.invalidate(serverName)
	}

	client := globalStdioClientPool.get(serverName, serverCfg)
	return client.invokeRaw(runCtx, method, params)
}

// invokeRaw performs an MCP request and returns the raw JSON result.
func (p *pooledStdioClient) invokeRaw(ctx context.Context, method string, params map[string]any) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.ensureStarted(); err != nil {
		return "", err
	}

	if !p.initialized {
		if _, err := p.client.request(ctx, "initialize", map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "goclaw",
				"version": "0.1.0",
			},
		}); err != nil {
			p.stopLocked()
			return "", fmt.Errorf("initialize failed: %w; stderr=%s", err, strings.TrimSpace(p.stderr.String()))
		}
		if err := p.client.notify("notifications/initialized", map[string]any{}); err != nil {
			p.stopLocked()
			return "", fmt.Errorf("initialized notify failed: %w", err)
		}
		p.initialized = true
	}

	result, err := p.client.request(ctx, method, params)
	if err != nil {
		p.stopLocked()
		return "", fmt.Errorf("%s failed: %w", method, err)
	}

	return string(result), nil
}
