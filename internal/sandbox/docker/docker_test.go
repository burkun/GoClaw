package docker

import (
	"os"
	"sync"
	"testing"
	"time"

	"goclaw/internal/sandbox"
)

// TestEffectiveContainerPrefix tests container name prefix generation
func TestEffectiveContainerPrefix(t *testing.T) {
	tests := []struct {
		name     string
		cfg      sandbox.SandboxConfig
		expected string
	}{
		{
			name:     "default prefix",
			cfg:      sandbox.SandboxConfig{},
			expected: defaultContainerNamePrefix,
		},
		{
			name: "custom prefix without dash",
			cfg: sandbox.SandboxConfig{
				Docker: sandbox.DockerConfig{ContainerPrefix: "custom-prefix"},
			},
			expected: "custom-prefix-",
		},
		{
			name: "custom prefix with dash",
			cfg: sandbox.SandboxConfig{
				Docker: sandbox.DockerConfig{ContainerPrefix: "custom-prefix-"},
			},
			expected: "custom-prefix-",
		},
		{
			name: "empty prefix uses default",
			cfg: sandbox.SandboxConfig{
				Docker: sandbox.DockerConfig{ContainerPrefix: ""},
			},
			expected: defaultContainerNamePrefix,
		},
		{
			name: "whitespace prefix uses default",
			cfg: sandbox.SandboxConfig{
				Docker: sandbox.DockerConfig{ContainerPrefix: "   "},
			},
			expected: defaultContainerNamePrefix,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := effectiveContainerPrefix(tt.cfg)
			if got != tt.expected {
				t.Errorf("effectiveContainerPrefix() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestBuildContainerEnvSorted tests environment variable building
func TestBuildContainerEnvSorted(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		expected []string
	}{
		{
			name:     "empty env",
			env:      map[string]string{},
			expected: nil,
		},
		{
			name: "sorted env vars",
			env: map[string]string{
				"B": "2",
				"A": "1",
			},
			expected: []string{"A=1", "B=2"},
		},
		{
			name: "skip empty keys",
			env: map[string]string{
				"B":  "2",
				"A":  "1",
				"":   "skip",
				"  ": "skip2",
			},
			expected: []string{"A=1", "B=2"},
		},
		{
			name: "single var",
			env: map[string]string{
				"PATH": "/usr/bin",
			},
			expected: []string{"PATH=/usr/bin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildContainerEnv(tt.env)
			if len(got) != len(tt.expected) {
				t.Errorf("buildContainerEnv() len = %d, want %d; got=%v", len(got), len(tt.expected), got)
				return
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("buildContainerEnv()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

// TestResolveEnvValue tests environment variable resolution
func TestResolveEnvValue(t *testing.T) {
	// Set test environment variables
	os.Setenv("TEST_VAR", "test_value")
	os.Setenv("TEST_VAR_WITH_DEFAULT", "actual_value")
	defer func() {
		os.Unsetenv("TEST_VAR")
		os.Unsetenv("TEST_VAR_WITH_DEFAULT")
	}()

	tests := []struct {
		name     string
		value    string
		expected string
	}{
		{
			name:     "plain value",
			value:    "plain_value",
			expected: "plain_value",
		},
		{
			name:     "env var exists",
			value:    "$TEST_VAR",
			expected: "test_value",
		},
		{
			name:     "env var with default, var exists",
			value:    "$TEST_VAR_WITH_DEFAULT:fallback",
			expected: "actual_value",
		},
		{
			name:     "env var with default, var missing",
			value:    "$MISSING_VAR:fallback",
			expected: "fallback",
		},
		{
			name:     "env var missing, no default",
			value:    "$MISSING_VAR_NO_DEFAULT",
			expected: "",
		},
		{
			name:     "empty string",
			value:    "",
			expected: "",
		},
		{
			name:     "whitespace string",
			value:    "   ",
			expected: "", // TrimSpace removes whitespace
		},
		{
			name:     "just dollar sign",
			value:    "$",
			expected: "$",
		},
		{
			name:     "colon in default value",
			value:    "$MISSING_VAR:default:value",
			expected: "default:value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveEnvValue(tt.value)
			if got != tt.expected {
				t.Errorf("resolveEnvValue(%q) = %q, want %q", tt.value, got, tt.expected)
			}
		})
	}
}

// TestRejectPathTraversal tests path traversal prevention
func TestRejectPathTraversal(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "safe path",
			path:    "/mnt/user-data/workspace/file.txt",
			wantErr: false,
		},
		{
			name:    "traversal with ..",
			path:    "/mnt/user-data/../etc/passwd",
			wantErr: true,
		},
		{
			name:    "hidden traversal",
			path:    "/mnt/user-data/workspace/../../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "backslash traversal",
			path:    "\\mnt\\user-data\\..\\etc\\passwd",
			wantErr: true,
		},
		{
			name:    "mixed slashes traversal",
			path:    "/mnt/user-data\\..\\etc/passwd",
			wantErr: true,
		},
		{
			name:    "current directory",
			path:    "/mnt/user-data/workspace/./file.txt",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rejectPathTraversal(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("rejectPathTraversal(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

// TestVirtualToContainerPath tests path translation
func TestVirtualToContainerPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		want    string
		wantErr bool
	}{
		{
			name:    "valid user-data path",
			path:    "/mnt/user-data/workspace/file.txt",
			want:    "/mnt/user-data/workspace/file.txt",
			wantErr: false,
		},
		{
			name:    "valid skills path",
			path:    "/mnt/skills/skill.py",
			want:    "/mnt/skills/skill.py",
			wantErr: false,
		},
		{
			name:    "invalid path outside allowed",
			path:    "/etc/passwd",
			want:    "",
			wantErr: true,
		},
		{
			name:    "traversal attempt",
			path:    "/mnt/user-data/../etc/passwd",
			want:    "",
			wantErr: true,
		},
		{
			name:    "root user-data",
			path:    "/mnt/user-data",
			want:    "/mnt/user-data",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := virtualToContainerPath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("virtualToContainerPath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("virtualToContainerPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// TestShellQuote tests shell quoting
func TestShellQuote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple string",
			input:    "hello",
			expected: "'hello'",
		},
		{
			name:     "string with spaces",
			input:    "hello world",
			expected: "'hello world'",
		},
		{
			name:     "string with single quote",
			input:    "it's",
			expected: "'it'\\''s'",
		},
		{
			name:     "string with multiple quotes",
			input:    "it's a 'test'",
			expected: "'it'\\''s a '\\''test'\\'''",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "''",
		},
		{
			name:     "path with special chars",
			input:    "/mnt/user-data/workspace/file.txt",
			expected: "'/mnt/user-data/workspace/file.txt'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellQuote(tt.input)
			if got != tt.expected {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestContainerName tests container name generation
func TestContainerName(t *testing.T) {
	tests := []struct {
		name     string
		cfg      sandbox.SandboxConfig
		threadID string
		expected string
	}{
		{
			name:     "default prefix",
			cfg:      sandbox.SandboxConfig{},
			threadID: "thread-123",
			expected: defaultContainerNamePrefix + "thread-123",
		},
		{
			name: "custom prefix",
			cfg: sandbox.SandboxConfig{
				Docker: sandbox.DockerConfig{ContainerPrefix: "custom"},
			},
			threadID: "thread-456",
			expected: "custom-thread-456",
		},
		{
			name:     "empty thread ID",
			cfg:      sandbox.SandboxConfig{},
			threadID: "",
			expected: defaultContainerNamePrefix,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containerName(tt.cfg, tt.threadID)
			if got != tt.expected {
				t.Errorf("containerName() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestMatchPatternDocker tests glob pattern matching
func TestMatchPatternDocker(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		pattern  string
		expected bool
	}{
		{
			name:     "empty pattern matches all",
			path:     "file.txt",
			pattern:  "",
			expected: true,
		},
		{
			name:     "simple match",
			path:     "file.txt",
			pattern:  "file.txt",
			expected: true,
		},
		{
			name:     "simple mismatch",
			path:     "file.txt",
			pattern:  "other.txt",
			expected: false,
		},
		{
			name:     "single wildcard",
			path:     "file.txt",
			pattern:  "*.txt",
			expected: true,
		},
		{
			name:     "single wildcard mismatch",
			path:     "file.go",
			pattern:  "*.txt",
			expected: false,
		},
		{
			name:     "question mark wildcard",
			path:     "file1.txt",
			pattern:  "file?.txt",
			expected: true,
		},
		{
			name:     "double star recursive",
			path:     "a/b/c/file.txt",
			pattern:  "**/*.txt",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchPatternDocker(tt.path, tt.pattern)
			if got != tt.expected {
				t.Errorf("matchPatternDocker(%q, %q) = %v, want %v", tt.path, tt.pattern, got, tt.expected)
			}
		})
	}
}

// TestGlobToRegexpDocker tests glob to regex conversion
func TestGlobToRegexpDocker(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		match   bool
	}{
		{
			name:    "double star matches nested",
			pattern: "**/*.go",
			path:    "a/b/c/file.go",
			match:   true,
		},

		{
			name:    "single star no slash match",
			pattern: "*.go",
			path:    "a/file.go",
			match:   false,
		},
		{
			name:    "special chars escaped",
			pattern: "file.test.go",
			path:    "file.test.go",
			match:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := globToRegexpDocker(tt.pattern)
			got := re.MatchString(tt.path)
			if got != tt.match {
				t.Errorf("globToRegexpDocker(%q).MatchString(%q) = %v, want %v", tt.pattern, tt.path, got, tt.match)
			}
		})
	}
}

// TestJoinVirtualPath tests virtual path joining
func TestJoinVirtualPath(t *testing.T) {
	tests := []struct {
		name     string
		root     string
		rel      string
		expected string
	}{
		{
			name:     "simple join",
			root:     "/mnt/user-data/workspace",
			rel:      "file.txt",
			expected: "/mnt/user-data/workspace/file.txt",
		},
		{
			name:     "rel with ./ prefix",
			root:     "/mnt/user-data/workspace",
			rel:      "./file.txt",
			expected: "/mnt/user-data/workspace/file.txt",
		},
		{
			name:     "rel with leading slash",
			root:     "/mnt/user-data/workspace",
			rel:      "/file.txt",
			expected: "/mnt/user-data/workspace/file.txt",
		},
		{
			name:     "empty rel",
			root:     "/mnt/user-data/workspace",
			rel:      "",
			expected: "/mnt/user-data/workspace",
		},
		{
			name:     "root with trailing slash",
			root:     "/mnt/user-data/workspace/",
			rel:      "file.txt",
			expected: "/mnt/user-data/workspace/file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinVirtualPath(tt.root, tt.rel)
			if got != tt.expected {
				t.Errorf("joinVirtualPath(%q, %q) = %q, want %q", tt.root, tt.rel, got, tt.expected)
			}
		})
	}
}

// TestMinIntDocker tests min integer function
func TestMinIntDocker(t *testing.T) {
	tests := []struct {
		a, b, expected int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{5, 5, 5},
		{0, 10, 0},
		{-1, 1, -1},
	}

	for _, tt := range tests {
		got := minIntDocker(tt.a, tt.b)
		if got != tt.expected {
			t.Errorf("minIntDocker(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.expected)
		}
	}
}

// TestDockerSandboxID tests the ID method
func TestDockerSandboxID(t *testing.T) {
	sb := &DockerSandbox{
		id:          "test-sandbox-123",
		threadID:    "thread-123",
		containerID: "container-456",
	}

	if sb.ID() != "test-sandbox-123" {
		t.Errorf("DockerSandbox.ID() = %q, want %q", sb.ID(), "test-sandbox-123")
	}
}

// TestDockerSandboxProvider tests provider lifecycle management
func TestDockerSandboxProvider_GetThreadLock(t *testing.T) {
	p := &DockerSandboxProvider{
		threadLocks: make(map[string]*sync.Mutex),
	}

	// First call should create lock
	lock1 := p.getThreadLock("thread-1")
	if lock1 == nil {
		t.Fatal("getThreadLock returned nil")
	}

	// Second call with same ID should return same lock
	lock2 := p.getThreadLock("thread-1")
	if lock1 != lock2 {
		t.Error("getThreadLock should return same lock for same thread ID")
	}

	// Different thread ID should get different lock
	lock3 := p.getThreadLock("thread-2")
	if lock1 == lock3 {
		t.Error("getThreadLock should return different locks for different thread IDs")
	}
}

// TestDockerSandboxProvider_DeterministicSandboxID tests deterministic ID generation
func TestDockerSandboxProvider_DeterministicSandboxID(t *testing.T) {
	p := &DockerSandboxProvider{
		cfg: sandbox.SandboxConfig{},
	}

	id1 := p.deterministicSandboxID("thread-123")
	id2 := p.deterministicSandboxID("thread-123")

	if id1 != id2 {
		t.Error("deterministicSandboxID should return same ID for same thread ID")
	}

	// Different thread should get different ID
	id3 := p.deterministicSandboxID("thread-456")
	if id1 == id3 {
		t.Error("deterministicSandboxID should return different IDs for different thread IDs")
	}
}

// TestDockerSandboxProvider_Get tests Get method
func TestDockerSandboxProvider_Get(t *testing.T) {
	p := &DockerSandboxProvider{
		sandboxes:    make(map[string]*DockerSandbox),
		lastActivity: make(map[string]time.Time),
	}

	// Get non-existent sandbox
	if sb := p.Get("non-existent"); sb != nil {
		t.Error("Get should return nil for non-existent sandbox")
	}

	// Add sandbox and retrieve it
	testSandbox := &DockerSandbox{
		id:       "test-sandbox",
		threadID: "thread-123",
	}
	p.sandboxes["test-sandbox"] = testSandbox

	sb := p.Get("test-sandbox")
	if sb == nil {
		t.Error("Get should return existing sandbox")
	}
	if sb.ID() != "test-sandbox" {
		t.Errorf("Get returned wrong sandbox: %q", sb.ID())
	}

	// Verify last activity was updated
	if _, ok := p.lastActivity["test-sandbox"]; !ok {
		t.Error("Get should update lastActivity")
	}
}
