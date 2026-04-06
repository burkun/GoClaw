// Package kubernetes provides a Kubernetes-based sandbox implementation.
//
// ARCHITECTURE (mirrors DeerFlow's provisioner mode):
// - Uses a separate Provisioner service (FastAPI) to manage Pod lifecycle
// - Each thread gets its own Pod running an aio-sandbox compatible container
// - Backend accesses sandbox pods directly via {NODE_HOST}:{NodePort}
// - File operations are executed via HTTP calls to the sandbox container
//
// Configuration (config.yaml):
//
//	sandbox:
//	  use: kubernetes
//	  provisioner_url: http://provisioner:8002
//	  image: enterprise-public-cn-beijing.cr.volces.com/vefaas-public/all-in-one-sandbox:latest
//	  namespace: deer-flow
//
// The provisioner service handles:
// - Pod creation with HostPath volumes for thread data
// - NodePort Service creation for network access
// - Pod lifecycle management (create/delete/list)
package kubernetes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bookerbai/goclaw/internal/sandbox"
)

// Default configuration values (aligned with DeerFlow).
const (
	defaultNamespace     = "deer-flow"
	defaultImage         = "enterprise-public-cn-beijing.cr.volces.com/vefaas-public/all-in-one-sandbox:latest"
	defaultIdleTimeout   = 600 // seconds
	defaultReplicas      = 3
	defaultCreateTimeout = 60 * time.Second
	defaultHTTPTimeout   = 30 * time.Second
)

// safeThreadIDPattern matches alphanumeric, hyphen, and underscore characters.
var safeThreadIDPattern = regexp.MustCompile(`^[A-Za-z0-9_\-]+$`)

// ---------------------------------------------------------------------------
// KubernetesSandbox (HTTP-based sandbox client)
// ---------------------------------------------------------------------------

// KubernetesSandbox runs commands inside a Kubernetes Pod via HTTP.
// It communicates with the sandbox container using the aio-sandbox HTTP API.
type KubernetesSandbox struct {
	id        string
	baseURL   string
	threadID  string
	lastUsed  time.Time
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	execCache map[string]cachedResult
}

type cachedResult struct {
	result sandbox.ExecuteResult
	err    error
}

// NewKubernetesSandbox creates a new Kubernetes-based sandbox client.
func NewKubernetesSandbox(id, baseURL, threadID string) *KubernetesSandbox {
	ctx, cancel := context.WithCancel(context.Background())
	return &KubernetesSandbox{
		id:        id,
		baseURL:   strings.TrimSuffix(baseURL, "/"),
		threadID:  threadID,
		lastUsed:  time.Now(),
		ctx:       ctx,
		cancel:    cancel,
		execCache: make(map[string]cachedResult),
	}
}

// ID returns the sandbox identifier.
func (s *KubernetesSandbox) ID() string {
	return s.id
}

// Execute runs a shell command inside the Pod via HTTP POST to /v1/execute.
func (s *KubernetesSandbox) Execute(ctx context.Context, command string) (sandbox.ExecuteResult, error) {
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()

	// Build execute request
	reqBody := executeRequest{
		Command: command,
		Timeout: 600, // default timeout in seconds
	}

	var resp executeResponse
	if err := s.doPost(ctx, "/v1/execute", reqBody, &resp); err != nil {
		return sandbox.ExecuteResult{Error: err}, nil
	}

	return sandbox.ExecuteResult{
		Stdout:   resp.Stdout,
		Stderr:   resp.Stderr,
		ExitCode: resp.ExitCode,
	}, nil
}

// ReadFile reads a file from the Pod via HTTP GET to /v1/files.
func (s *KubernetesSandbox) ReadFile(ctx context.Context, path string, startLine, endLine int) (string, error) {
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()

	// Build URL with query parameters
	url := fmt.Sprintf("%s/v1/files?path=%s", s.baseURL, urlEncode(path))
	if startLine > 0 || endLine > 0 {
		url += fmt.Sprintf("&start_line=%d&end_line=%d", startLine, endLine)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("read file failed (status %d): %s", resp.StatusCode, string(body))
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	return string(content), nil
}

// WriteFile writes content to a file in the Pod via HTTP POST to /v1/files.
func (s *KubernetesSandbox) WriteFile(ctx context.Context, path string, content string, appendMode bool) error {
	return sandbox.WithFileLock(s.id, path, func() error {
		return s.writeFileLocked(ctx, path, content, appendMode)
	})
}

func (s *KubernetesSandbox) writeFileLocked(ctx context.Context, path string, content string, appendMode bool) error {
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()

	reqBody := writeFileRequest{
		Path:      path,
		Content:   base64.StdEncoding.EncodeToString([]byte(content)),
		Append:    appendMode,
		Encoding:  "base64",
		CreateDir: true,
	}

	var resp writeFileResponse
	if err := s.doPost(ctx, "/v1/files", reqBody, &resp); err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("write file failed: %s", resp.Error)
	}
	return nil
}

// ListDir lists directory contents in the Pod via HTTP GET to /v1/files/list.
func (s *KubernetesSandbox) ListDir(ctx context.Context, path string, maxDepth int) ([]sandbox.FileInfo, error) {
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()

	if maxDepth <= 0 {
		maxDepth = 2
	}

	url := fmt.Sprintf("%s/v1/files/list?path=%s&max_depth=%d", s.baseURL, urlEncode(path), maxDepth)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list dir: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list dir failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result listDirResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Convert to sandbox.FileInfo
	infos := make([]sandbox.FileInfo, len(result.Files))
	for i, f := range result.Files {
		infos[i] = sandbox.FileInfo{
			Name:    f.Name,
			Path:    f.Path,
			Size:    f.Size,
			IsDir:   f.IsDir,
			ModTime: time.Unix(f.ModTime, 0),
		}
	}
	return infos, nil
}

// StrReplace replaces text in a file in the Pod.
func (s *KubernetesSandbox) StrReplace(ctx context.Context, path string, oldStr string, newStr string, replaceAll bool) error {
	return sandbox.WithFileLock(s.id, path, func() error {
		return s.strReplaceLocked(ctx, path, oldStr, newStr, replaceAll)
	})
}

func (s *KubernetesSandbox) strReplaceLocked(ctx context.Context, path string, oldStr string, newStr string, replaceAll bool) error {
	content, err := s.ReadFile(ctx, path, 0, 0)
	if err != nil {
		return err
	}
	if !strings.Contains(content, oldStr) {
		return fmt.Errorf("str_replace: string to replace not found in %q", path)
	}
	n := 1
	if replaceAll {
		n = -1
	}
	newContent := strings.Replace(content, oldStr, newStr, n)
	return s.writeFileLocked(ctx, path, newContent, false)
}

// Glob finds files matching a pattern in the Pod via HTTP GET to /v1/files/glob.
func (s *KubernetesSandbox) Glob(ctx context.Context, path string, pattern string, includeDirs bool, maxResults int) ([]string, bool, error) {
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()

	if maxResults <= 0 {
		maxResults = 200
	}

	url := fmt.Sprintf("%s/v1/files/glob?path=%s&pattern=%s&include_dirs=%t&max_results=%d",
		s.baseURL, urlEncode(path), urlEncode(pattern), includeDirs, maxResults)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("glob: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, false, fmt.Errorf("glob failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result globResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, false, fmt.Errorf("decode response: %w", err)
	}

	return result.Matches, result.Truncated, nil
}

// Grep searches for pattern matches in files in the Pod via HTTP GET to /v1/files/grep.
func (s *KubernetesSandbox) Grep(ctx context.Context, path string, pattern string, glob string, literal bool, caseSensitive bool, maxResults int) ([]sandbox.GrepMatch, bool, error) {
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()

	if maxResults <= 0 {
		maxResults = 100
	}

	url := fmt.Sprintf("%s/v1/files/grep?path=%s&pattern=%s&literal=%t&case_sensitive=%t&max_results=%d",
		s.baseURL, urlEncode(path), urlEncode(pattern), literal, caseSensitive, maxResults)
	if glob != "" {
		url += "&glob=" + urlEncode(glob)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("grep: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, false, fmt.Errorf("grep failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result grepResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, false, fmt.Errorf("decode response: %w", err)
	}

	// Convert to sandbox.GrepMatch
	matches := make([]sandbox.GrepMatch, len(result.Matches))
	for i, m := range result.Matches {
		matches[i] = sandbox.GrepMatch{
			Path:       m.Path,
			LineNumber: m.LineNumber,
			Line:       m.Line,
		}
	}
	return matches, result.Truncated, nil
}

// UpdateFile writes binary content to a file in the Pod.
func (s *KubernetesSandbox) UpdateFile(ctx context.Context, path string, content []byte) error {
	return sandbox.WithFileLock(s.id, path, func() error {
		return s.updateFileLocked(ctx, path, content)
	})
}

func (s *KubernetesSandbox) updateFileLocked(ctx context.Context, path string, content []byte) error {
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()

	reqBody := writeFileRequest{
		Path:      path,
		Content:   base64.StdEncoding.EncodeToString(content),
		Encoding:  "base64",
		CreateDir: true,
	}

	var resp writeFileResponse
	if err := s.doPost(ctx, "/v1/files", reqBody, &resp); err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("update file failed: %s", resp.Error)
	}
	return nil
}

// doPost is a helper to make POST requests to the sandbox.
func (s *KubernetesSandbox) doPost(ctx context.Context, endpoint string, reqBody any, respBody any) error {
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("post %s failed (status %d): %s", endpoint, resp.StatusCode, string(body))
	}

	if respBody != nil {
		if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// Close cleans up the sandbox client resources.
func (s *KubernetesSandbox) Close() {
	s.cancel()
}

// ---------------------------------------------------------------------------
// Request/Response types for sandbox HTTP API
// ---------------------------------------------------------------------------

type executeRequest struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

type executeResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

type writeFileRequest struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Append    bool   `json:"append,omitempty"`
	Encoding  string `json:"encoding,omitempty"`
	CreateDir bool   `json:"create_dir,omitempty"`
}

type writeFileResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type fileInfoResponse struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"is_dir"`
	ModTime int64  `json:"mod_time"`
}

type listDirResponse struct {
	Files []fileInfoResponse `json:"files"`
}

type globResponse struct {
	Matches   []string `json:"matches"`
	Truncated bool     `json:"truncated"`
}

type grepMatchResponse struct {
	Path       string `json:"path"`
	LineNumber int    `json:"line_number"`
	Line       string `json:"line"`
}

type grepResponse struct {
	Matches   []grepMatchResponse `json:"matches"`
	Truncated bool                `json:"truncated"`
}

// ---------------------------------------------------------------------------
// KubernetesSandboxProvider (Provisioner-based)
// ---------------------------------------------------------------------------

// KubernetesSandboxProvider manages Kubernetes sandbox lifecycles via the Provisioner service.
type KubernetesSandboxProvider struct {
	provisionerURL string
	namespace      string
	image          string
	nodeHost       string

	mu              sync.Mutex
	sandboxes       map[string]*KubernetesSandbox // sandbox_id -> sandbox
	sandboxInfos    map[string]*SandboxInfo       // sandbox_id -> info
	threadSandboxes map[string]string             // thread_id -> sandbox_id
	lastActivity    map[string]time.Time          // sandbox_id -> last activity
	warmPool        map[string]warmPoolEntry      // sandbox_id -> warm pool entry
	replicas        int
	idleTimeout     time.Duration
	shutdownOnce    sync.Once
}

type warmPoolEntry struct {
	info       *SandboxInfo
	releasedAt time.Time
}

// SandboxInfo contains metadata about a sandbox pod.
type SandboxInfo struct {
	SandboxID  string    `json:"sandbox_id"`
	SandboxURL string    `json:"sandbox_url"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
}

// NewKubernetesSandboxProvider creates a provider that uses the Provisioner service.
func NewKubernetesSandboxProvider(provisionerURL, namespace, image, nodeHost string, replicas int, idleTimeout time.Duration) *KubernetesSandboxProvider {
	if namespace == "" {
		namespace = defaultNamespace
	}
	if image == "" {
		image = defaultImage
	}
	if nodeHost == "" {
		nodeHost = "host.docker.internal"
	}
	if replicas <= 0 {
		replicas = defaultReplicas
	}
	if idleTimeout <= 0 {
		idleTimeout = defaultIdleTimeout * time.Second
	}

	return &KubernetesSandboxProvider{
		provisionerURL:  strings.TrimSuffix(provisionerURL, "/"),
		namespace:       namespace,
		image:           image,
		nodeHost:        nodeHost,
		sandboxes:       make(map[string]*KubernetesSandbox),
		sandboxInfos:    make(map[string]*SandboxInfo),
		threadSandboxes: make(map[string]string),
		lastActivity:    make(map[string]time.Time),
		warmPool:        make(map[string]warmPoolEntry),
		replicas:        replicas,
		idleTimeout:     idleTimeout,
	}
}

// Acquire creates or reuses a Pod for the given thread.
func (p *KubernetesSandboxProvider) Acquire(ctx context.Context, threadID string) (string, error) {
	if threadID == "" {
		return "", fmt.Errorf("thread_id is required for Kubernetes sandbox")
	}

	// Validate thread ID
	if !safeThreadIDPattern.MatchString(threadID) {
		return "", fmt.Errorf("invalid thread_id: only alphanumeric characters, hyphens, and underscores are allowed")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if sandbox already exists for this thread
	if existingID, ok := p.threadSandboxes[threadID]; ok {
		if _, exists := p.sandboxes[existingID]; exists {
			p.lastActivity[existingID] = time.Now()
			return existingID, nil
		}
		// Clean up stale mapping
		delete(p.threadSandboxes, threadID)
	}

	// Generate deterministic sandbox ID
	sandboxID := deterministicSandboxID(threadID)

	// Check warm pool
	if entry, ok := p.warmPool[sandboxID]; ok {
		delete(p.warmPool, sandboxID)
		sb := NewKubernetesSandbox(sandboxID, entry.info.SandboxURL, threadID)
		p.sandboxes[sandboxID] = sb
		p.sandboxInfos[sandboxID] = entry.info
		p.lastActivity[sandboxID] = time.Now()
		p.threadSandboxes[threadID] = sandboxID
		return sandboxID, nil
	}

	// Check if sandbox already exists via provisioner
	existing, err := p.discoverSandbox(ctx, sandboxID)
	if err == nil && existing != nil {
		sb := NewKubernetesSandbox(sandboxID, existing.SandboxURL, threadID)
		p.sandboxes[sandboxID] = sb
		p.sandboxInfos[sandboxID] = existing
		p.lastActivity[sandboxID] = time.Now()
		p.threadSandboxes[threadID] = sandboxID
		return sandboxID, nil
	}

	// Enforce replicas limit
	total := len(p.sandboxes) + len(p.warmPool)
	if total >= p.replicas {
		if evicted := p.evictOldestWarm(); evicted != "" {
			// Successfully evicted
		}
	}

	// Create new sandbox via provisioner
	info, err := p.createSandbox(ctx, sandboxID, threadID)
	if err != nil {
		return "", fmt.Errorf("create sandbox: %w", err)
	}

	// Wait for sandbox to be ready
	if !p.waitForSandboxReady(ctx, info.SandboxURL, 60*time.Second) {
		_ = p.destroySandbox(ctx, info.SandboxID)
		return "", fmt.Errorf("sandbox %s failed to become ready", sandboxID)
	}

	sb := NewKubernetesSandbox(sandboxID, info.SandboxURL, threadID)
	p.sandboxes[sandboxID] = sb
	p.sandboxInfos[sandboxID] = info
	p.lastActivity[sandboxID] = time.Now()
	p.threadSandboxes[threadID] = sandboxID

	return sandboxID, nil
}

// Release moves a sandbox from active use to the warm pool.
func (p *KubernetesSandboxProvider) Release(ctx context.Context, sandboxID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Remove from active maps
	delete(p.sandboxes, sandboxID)
	info := p.sandboxInfos[sandboxID]

	// Remove thread mapping
	for tid, sid := range p.threadSandboxes {
		if sid == sandboxID {
			delete(p.threadSandboxes, tid)
			break
		}
	}
	delete(p.lastActivity, sandboxID)

	// Add to warm pool if info exists
	if info != nil {
		p.warmPool[sandboxID] = warmPoolEntry{
			info:       info,
			releasedAt: time.Now(),
		}
	}

	return nil
}

// Get retrieves an existing sandbox by its ID.
func (p *KubernetesSandboxProvider) Get(sandboxID string) sandbox.Sandbox {
	p.mu.Lock()
	defer p.mu.Unlock()

	sb := p.sandboxes[sandboxID]
	if sb != nil {
		p.lastActivity[sandboxID] = time.Now()
	}
	return sb
}

// Shutdown tears down all active sandboxes.
func (p *KubernetesSandboxProvider) Shutdown(ctx context.Context) error {
	var err error
	p.shutdownOnce.Do(func() {
		p.mu.Lock()
		sandboxIDs := make([]string, 0, len(p.sandboxes))
		for id := range p.sandboxes {
			sandboxIDs = append(sandboxIDs, id)
		}
		warmEntries := make([]struct {
			id   string
			info *SandboxInfo
		}, 0, len(p.warmPool))
		for id, entry := range p.warmPool {
			warmEntries = append(warmEntries, struct {
				id   string
				info *SandboxInfo
			}{id, entry.info})
		}
		p.mu.Unlock()

		// Destroy active sandboxes
		for _, id := range sandboxIDs {
			if e := p.Destroy(ctx, id); e != nil {
				err = e
			}
		}

		// Destroy warm pool entries
		for _, entry := range warmEntries {
			if e := p.destroySandbox(ctx, entry.id); e != nil {
				err = e
			}
		}
	})
	return err
}

// Destroy completely removes a sandbox (stops container).
func (p *KubernetesSandboxProvider) Destroy(ctx context.Context, sandboxID string) error {
	p.mu.Lock()
	delete(p.sandboxes, sandboxID)
	info := p.sandboxInfos[sandboxID]
	delete(p.sandboxInfos, sandboxID)
	for tid, sid := range p.threadSandboxes {
		if sid == sandboxID {
			delete(p.threadSandboxes, tid)
			break
		}
	}
	delete(p.lastActivity, sandboxID)
	delete(p.warmPool, sandboxID)
	p.mu.Unlock()

	if info != nil {
		return p.destroySandbox(ctx, sandboxID)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Provisioner API calls
// ---------------------------------------------------------------------------

func (p *KubernetesSandboxProvider) createSandbox(ctx context.Context, sandboxID, threadID string) (*SandboxInfo, error) {
	reqBody := provisionerCreateRequest{
		SandboxID: sandboxID,
		ThreadID:  threadID,
	}

	var resp provisionerSandboxResponse
	if err := p.postProvisioner(ctx, "/api/sandboxes", reqBody, &resp); err != nil {
		return nil, err
	}

	return &SandboxInfo{
		SandboxID:  resp.SandboxID,
		SandboxURL: resp.SandboxURL,
		Status:     resp.Status,
		CreatedAt:  time.Now(),
	}, nil
}

func (p *KubernetesSandboxProvider) destroySandbox(ctx context.Context, sandboxID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		fmt.Sprintf("%s/api/sandboxes/%s", p.provisionerURL, sandboxID), nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("destroy sandbox failed (status %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

func (p *KubernetesSandboxProvider) discoverSandbox(ctx context.Context, sandboxID string) (*SandboxInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/api/sandboxes/%s", p.provisionerURL, sandboxID), nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("discover sandbox failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result provisionerSandboxResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &SandboxInfo{
		SandboxID:  result.SandboxID,
		SandboxURL: result.SandboxURL,
		Status:     result.Status,
	}, nil
}

func (p *KubernetesSandboxProvider) postProvisioner(ctx context.Context, endpoint string, reqBody any, respBody any) error {
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.provisionerURL+endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: defaultHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("post %s failed (status %d): %s", endpoint, resp.StatusCode, string(body))
	}

	if respBody != nil {
		if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func (p *KubernetesSandboxProvider) waitForSandboxReady(ctx context.Context, sandboxURL string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, sandboxURL+"/v1/sandbox", nil)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}

		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(1 * time.Second)
	}
	return false
}

func (p *KubernetesSandboxProvider) evictOldestWarm() string {
	if len(p.warmPool) == 0 {
		return ""
	}

	// Find oldest entry
	var oldestID string
	var oldestTime time.Time
	for id, entry := range p.warmPool {
		if oldestID == "" || entry.releasedAt.Before(oldestTime) {
			oldestID = id
			oldestTime = entry.releasedAt
		}
	}

	if oldestID == "" {
		return ""
	}

	// Remove from warm pool and destroy
	delete(p.warmPool, oldestID)
	_ = p.destroySandbox(context.Background(), oldestID)
	return oldestID
}

// ---------------------------------------------------------------------------
// Provisioner request/response types
// ---------------------------------------------------------------------------

type provisionerCreateRequest struct {
	SandboxID string `json:"sandbox_id"`
	ThreadID  string `json:"thread_id"`
}

type provisionerSandboxResponse struct {
	SandboxID  string `json:"sandbox_id"`
	SandboxURL string `json:"sandbox_url"`
	Status     string `json:"status"`
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// deterministicSandboxID generates a deterministic sandbox ID from a thread ID.
// This matches DeerFlow's hashlib.sha256(thread_id.encode()).hexdigest()[:8]
func deterministicSandboxID(threadID string) string {
	h := sha256.Sum256([]byte(threadID))
	return hex.EncodeToString(h[:])[:8]
}

func urlEncode(s string) string {
	// Basic URL encoding
	s = strings.ReplaceAll(s, " ", "%20")
	s = strings.ReplaceAll(s, "&", "%26")
	s = strings.ReplaceAll(s, "=", "%3D")
	return s
}

// Ensure KubernetesSandboxProvider implements SandboxProvider interface.
var _ sandbox.SandboxProvider = (*KubernetesSandboxProvider)(nil)

// Ensure KubernetesSandbox implements Sandbox interface.
var _ sandbox.Sandbox = (*KubernetesSandbox)(nil)
