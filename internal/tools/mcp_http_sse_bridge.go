package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bookerbai/goclaw/internal/config"
)

const defaultMCPHTTPTimeout = 30 * time.Second

type mcpRPCClient interface {
	request(ctx context.Context, method string, params any) (json.RawMessage, error)
	notify(method string, params any) error
}

type mcpHTTPClient struct {
	httpClient *http.Client
	url        string
	headers    map[string]string
	nextID     int
}

type mcpSSEClient struct {
	httpClient *http.Client
	postURL    string
	headers    map[string]string
	stream     *bufio.Reader
	nextID     int
}

type sseEvent struct {
	event string
	data  string
}

func invokeMCPHTTP(ctx context.Context, serverName string, serverCfg config.MCPServerConfig, in mcpToolInput) (string, error) {
	endpoint := strings.TrimSpace(serverCfg.URL)
	if endpoint == "" {
		return "", fmt.Errorf("http server %q requires url", serverName)
	}

	runCtx := ctx
	cancel := func() {}
	if _, ok := runCtx.Deadline(); !ok {
		var c context.CancelFunc
		runCtx, c = context.WithTimeout(ctx, defaultMCPHTTPTimeout)
		cancel = c
	}
	defer cancel()

	cli := &mcpHTTPClient{
		httpClient: &http.Client{Timeout: defaultMCPHTTPTimeout},
		url:        endpoint,
		headers:    copyHeaders(serverCfg.Headers),
	}
	return invokeMCPByRPCClient(runCtx, cli, in)
}

func invokeMCPSSE(ctx context.Context, serverName string, serverCfg config.MCPServerConfig, in mcpToolInput) (string, error) {
	streamURL := strings.TrimSpace(serverCfg.URL)
	if streamURL == "" {
		return "", fmt.Errorf("sse server %q requires url", serverName)
	}

	runCtx := ctx
	cancel := func() {}
	if _, ok := runCtx.Deadline(); !ok {
		var c context.CancelFunc
		runCtx, c = context.WithTimeout(ctx, defaultMCPHTTPTimeout)
		cancel = c
	}
	defer cancel()

	httpClient := &http.Client{Timeout: defaultMCPHTTPTimeout}
	resp, err := openSSEStream(runCtx, httpClient, streamURL, serverCfg.Headers)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	postURL, err := discoverSSEPostURL(runCtx, reader, streamURL)
	if err != nil {
		postURL = streamURL
	}

	cli := &mcpSSEClient{
		httpClient: httpClient,
		postURL:    postURL,
		headers:    copyHeaders(serverCfg.Headers),
		stream:     reader,
	}
	return invokeMCPByRPCClient(runCtx, cli, in)
}

func invokeMCPByRPCClient(ctx context.Context, client mcpRPCClient, in mcpToolInput) (string, error) {
	if _, err := client.request(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "goclaw",
			"version": "0.1.0",
		},
	}); err != nil {
		return "", fmt.Errorf("initialize failed: %w", err)
	}
	if err := client.notify("notifications/initialized", map[string]any{}); err != nil {
		return "", fmt.Errorf("initialized notify failed: %w", err)
	}

	listRaw, err := client.request(ctx, "tools/list", map[string]any{})
	if err != nil {
		return "", fmt.Errorf("tools/list failed: %w", err)
	}
	if err := ensureMCPToolExposed(listRaw, in.ToolName); err != nil {
		return "", err
	}

	callRaw, err := client.request(ctx, "tools/call", map[string]any{
		"name":      in.ToolName,
		"arguments": defaultMCPArguments(in.Arguments),
	})
	if err != nil {
		return "", fmt.Errorf("tools/call failed: %w", err)
	}
	return formatMCPCallResult(callRaw)
}

func (c *mcpHTTPClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.nextID++
	id := c.nextID
	idCopy := id
	msg := mcpEnvelope{JSONRPC: "2.0", ID: &idCopy, Method: method}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		msg.Params = b
	}

	body, err := postRPCMessage(ctx, c.httpClient, c.url, c.headers, msg)
	if err != nil {
		return nil, err
	}
	return parseRPCResponse(body, id)
}

func (c *mcpHTTPClient) notify(method string, params any) error {
	msg := mcpEnvelope{JSONRPC: "2.0", Method: method}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal params: %w", err)
		}
		msg.Params = b
	}
	_, err := postRPCMessage(context.Background(), c.httpClient, c.url, c.headers, msg)
	return err
}

func (c *mcpSSEClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.nextID++
	cID := c.nextID
	idCopy := cID
	msg := mcpEnvelope{JSONRPC: "2.0", ID: &idCopy, Method: method}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		msg.Params = b
	}

	body, err := postRPCMessage(ctx, c.httpClient, c.postURL, c.headers, msg)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(body)) != 0 {
		if out, err := parseRPCResponse(body, cID); err == nil {
			return out, nil
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		ev, err := readSSEEvent(c.stream)
		if err != nil {
			return nil, fmt.Errorf("read sse event: %w", err)
		}
		if strings.TrimSpace(ev.data) == "" {
			continue
		}
		var env mcpEnvelope
		if err := json.Unmarshal([]byte(ev.data), &env); err != nil {
			continue
		}
		if env.ID == nil || *env.ID != cID {
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

func (c *mcpSSEClient) notify(method string, params any) error {
	msg := mcpEnvelope{JSONRPC: "2.0", Method: method}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal params: %w", err)
		}
		msg.Params = b
	}
	_, err := postRPCMessage(context.Background(), c.httpClient, c.postURL, c.headers, msg)
	return err
}

func postRPCMessage(ctx context.Context, client *http.Client, endpoint string, headers map[string]string, msg mcpEnvelope) ([]byte, error) {
	payload, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal rpc message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range headers {
		if strings.TrimSpace(k) == "" {
			continue
		}
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func parseRPCResponse(body []byte, expectedID int) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return json.RawMessage("{}"), nil
	}

	var env mcpEnvelope
	if err := json.Unmarshal(trimmed, &env); err != nil {
		return trimmed, nil
	}
	if env.JSONRPC != "2.0" {
		return trimmed, nil
	}
	if env.Error != nil {
		return nil, fmt.Errorf("rpc error code=%d message=%s", env.Error.Code, env.Error.Message)
	}
	if env.ID != nil && *env.ID != expectedID {
		return nil, fmt.Errorf("rpc response id mismatch: got=%d want=%d", *env.ID, expectedID)
	}
	if len(env.Result) == 0 {
		return json.RawMessage("{}"), nil
	}
	return env.Result, nil
}

func openSSEStream(ctx context.Context, client *http.Client, endpoint string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build sse request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	for k, v := range headers {
		if strings.TrimSpace(k) == "" {
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("open sse stream: %w", err)
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("open sse stream failed status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

func discoverSSEPostURL(ctx context.Context, reader *bufio.Reader, streamURL string) (string, error) {
	base, err := url.Parse(streamURL)
	if err != nil {
		return "", fmt.Errorf("invalid sse stream url: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		ev, err := readSSEEvent(reader)
		if err != nil {
			return "", err
		}
		if ev.event != "endpoint" && ev.event != "" {
			continue
		}
		endpoint := strings.TrimSpace(ev.data)
		if endpoint == "" {
			continue
		}
		if m := parseSSEEndpointEvent(endpoint); m != "" {
			endpoint = m
		}
		u, err := url.Parse(endpoint)
		if err != nil {
			continue
		}
		if !u.IsAbs() {
			u = base.ResolveReference(u)
		}
		return u.String(), nil
	}
}

func parseSSEEndpointEvent(data string) string {
	data = strings.TrimSpace(data)
	var obj map[string]any
	if err := json.Unmarshal([]byte(data), &obj); err == nil {
		if s, ok := obj["endpoint"].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
		if s, ok := obj["url"].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	if strings.HasPrefix(data, "http://") || strings.HasPrefix(data, "https://") || strings.HasPrefix(data, "/") {
		return data
	}
	return ""
}

func readSSEEvent(reader *bufio.Reader) (sseEvent, error) {
	var (
		eventName string
		dataLines []string
	)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return sseEvent{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if len(dataLines) == 0 && eventName == "" {
				continue
			}
			return sseEvent{event: eventName, data: strings.Join(dataLines, "\n")}, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
}

func copyHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
