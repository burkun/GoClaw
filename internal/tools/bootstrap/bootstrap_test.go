package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/internal/sandbox"
	localsandbox "github.com/bookerbai/goclaw/internal/sandbox/local"
	"github.com/bookerbai/goclaw/internal/tools"
	"github.com/bookerbai/goclaw/internal/tools/builtin"
	fstools "github.com/bookerbai/goclaw/internal/tools/fs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterDefaultTools_RegistersCoreTools(t *testing.T) {
	// 重置默认注册表
	tools.ResetDefaultRegistry()

	cfg := &config.AppConfig{
		Sandbox: config.SandboxConfig{
			Use: "local",
		},
	}

	err := RegisterDefaultTools(cfg)
	require.NoError(t, err)

	// 验证核心工具已注册
	expectedTools := []string{
		"read_file",
		"write_file",
		"edit_file",
		"list_dir",
		"glob",
		"grep",
		"bash",
		"web_search",
		"web_fetch",
		"image_search",
		"present_files",
		"ask_clarification",
		"tool_search",
		"task",
		"setup_agent",
	}

	for _, name := range expectedTools {
		tool, ok := tools.Get(name)
		assert.True(t, ok, "expected tool %q to be registered", name)
		if ok {
			assert.NotEmpty(t, tool.Name())
			assert.NotEmpty(t, tool.Description())
		}
	}
}

func TestRegisterDefaultTools_WithModelVision(t *testing.T) {
	// 重置默认注册表
	tools.ResetDefaultRegistry()

	cfg := &config.AppConfig{
		Sandbox: config.SandboxConfig{
			Use: "local",
		},
	}

	// 测试有vision支持的模型配置
	modelCfg := &config.ModelConfig{
		SupportsVision: true,
	}

	err := RegisterDefaultToolsWithModel(cfg, modelCfg)
	require.NoError(t, err)

	// 验证view_image工具在vision支持时被注册
	_, ok := tools.Get("view_image")
	assert.True(t, ok, "expected view_image to be registered when model supports vision")
}

func TestRegisterDefaultTools_WithoutModelVision(t *testing.T) {
	// 重置默认注册表
	tools.ResetDefaultRegistry()

	cfg := &config.AppConfig{
		Sandbox: config.SandboxConfig{
			Use: "local",
		},
	}

	// 测试不支持vision的模型配置
	modelCfg := &config.ModelConfig{
		SupportsVision: false,
	}

	err := RegisterDefaultToolsWithModel(cfg, modelCfg)
	require.NoError(t, err)

	// 验证view_image工具在vision不支持时不会被注册
	_, ok := tools.Get("view_image")
	assert.False(t, ok, "expected view_image NOT to be registered when model doesn't support vision")
}

func TestRegisterDefaultTools_GlobAndGrepExecute(t *testing.T) {
	// 保存当前目录
	cwd, _ := os.Getwd()
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	// 创建workspace目录和测试文件
	workspace := filepath.Join(".goclaw", "threads", "default", "user-data", "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "test.txt"), []byte("hello world\ntest content\nhello again\n"), 0o644); err != nil {
		t.Fatalf("write fixture failed: %v", err)
	}

	// 设置sandbox provider
	provider := localsandbox.NewLocalSandboxProvider(
		sandbox.SandboxConfig{Type: sandbox.SandboxTypeLocal, WorkDir: ".goclaw"},
		".goclaw",
		"",
	)
	sandbox.SetDefaultProvider(provider)
	t.Cleanup(func() { sandbox.SetDefaultProvider(nil) })

	// 注册默认工具
	if err := RegisterDefaultTools(&config.AppConfig{}); err != nil {
		t.Fatalf("register default tools failed: %v", err)
	}

	// 测试glob工具
	globTool, ok := tools.Get("glob")
	require.True(t, ok, "glob tool should be registered")

	globOut, err := globTool.Execute(context.Background(), `{"description":"test","pattern":"*.txt","path":"/mnt/user-data/workspace"}`)
	require.NoError(t, err)
	assert.Contains(t, globOut, "test.txt", "expected glob output to contain test.txt")

	// 测试grep工具
	grepTool, ok := tools.Get("grep")
	require.True(t, ok, "grep tool should be registered")

	grepOut, err := grepTool.Execute(context.Background(), `{"description":"test","pattern":"hello","path":"/mnt/user-data/workspace","literal":true}`)
	require.NoError(t, err)
	assert.Contains(t, grepOut, "hello", "expected grep output to contain 'hello'")
}

func TestReadFileRuntimeTool(t *testing.T) {
	// 保存当前目录
	cwd, _ := os.Getwd()
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	// 创建workspace目录和测试文件
	workspace := filepath.Join(".goclaw", "threads", "default", "user-data", "workspace")
	require.NoError(t, os.MkdirAll(workspace, 0o755))
	testContent := "test file content"
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "read_test.txt"), []byte(testContent), 0o644))

	// 设置sandbox provider
	provider := localsandbox.NewLocalSandboxProvider(
		sandbox.SandboxConfig{Type: sandbox.SandboxTypeLocal, WorkDir: ".goclaw"},
		".goclaw",
		"",
	)
	sandbox.SetDefaultProvider(provider)
	t.Cleanup(func() { sandbox.SetDefaultProvider(nil) })

	// 注册默认工具
	cfg := &config.AppConfig{}
	require.NoError(t, RegisterDefaultTools(cfg))

	// 获取read_file工具
	readTool, ok := tools.Get("read_file")
	require.True(t, ok, "read_file tool should be registered")

	// 执行读取
	result, err := readTool.Execute(context.Background(), `{"description":"test","path":"/mnt/user-data/workspace/read_test.txt"}`)
	require.NoError(t, err)
	assert.Equal(t, testContent, result)
}

func TestWriteFileRuntimeTool(t *testing.T) {
	// 保存当前目录
	cwd, _ := os.Getwd()
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	// 创建workspace目录
	workspace := filepath.Join(".goclaw", "threads", "default", "user-data", "workspace")
	require.NoError(t, os.MkdirAll(workspace, 0o755))

	// 设置sandbox provider
	provider := localsandbox.NewLocalSandboxProvider(
		sandbox.SandboxConfig{Type: sandbox.SandboxTypeLocal, WorkDir: ".goclaw"},
		".goclaw",
		"",
	)
	sandbox.SetDefaultProvider(provider)
	t.Cleanup(func() { sandbox.SetDefaultProvider(nil) })

	// 注册默认工具
	cfg := &config.AppConfig{}
	require.NoError(t, RegisterDefaultTools(cfg))

	// 获取write_file工具
	writeTool, ok := tools.Get("write_file")
	require.True(t, ok, "write_file tool should be registered")

	// 执行写入
	testContent := "written content"
	result, err := writeTool.Execute(context.Background(), `{"description":"test","path":"/mnt/user-data/workspace/write_test.txt","content":"`+testContent+`"}`)
	require.NoError(t, err)
	assert.Equal(t, "OK", result)

	// 验证文件已写入
	content, err := os.ReadFile(filepath.Join(workspace, "write_test.txt"))
	require.NoError(t, err)
	assert.Equal(t, testContent, string(content))
}

func TestEditFileRuntimeTool(t *testing.T) {
	// 保存当前目录
	cwd, _ := os.Getwd()
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	// 创建workspace目录和测试文件
	workspace := filepath.Join(".goclaw", "threads", "default", "user-data", "workspace")
	require.NoError(t, os.MkdirAll(workspace, 0o755))
	originalContent := "old content here"
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "edit_test.txt"), []byte(originalContent), 0o644))

	// 设置sandbox provider
	provider := localsandbox.NewLocalSandboxProvider(
		sandbox.SandboxConfig{Type: sandbox.SandboxTypeLocal, WorkDir: ".goclaw"},
		".goclaw",
		"",
	)
	sandbox.SetDefaultProvider(provider)
	t.Cleanup(func() { sandbox.SetDefaultProvider(nil) })

	// 注册默认工具
	cfg := &config.AppConfig{}
	require.NoError(t, RegisterDefaultTools(cfg))

	// 获取edit_file工具
	editTool, ok := tools.Get("edit_file")
	require.True(t, ok, "edit_file tool should be registered")

	// 执行编辑
	result, err := editTool.Execute(context.Background(), `{"description":"test","path":"/mnt/user-data/workspace/edit_test.txt","old_str":"old content","new_str":"new content"}`)
	require.NoError(t, err)
	assert.Equal(t, "OK", result)

	// 验证文件已编辑
	content, err := os.ReadFile(filepath.Join(workspace, "edit_test.txt"))
	require.NoError(t, err)
	assert.Equal(t, "new content here", string(content))
}

func TestListDirRuntimeTool(t *testing.T) {
	// 保存当前目录
	cwd, _ := os.Getwd()
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	// 创建workspace目录和子目录
	workspace := filepath.Join(".goclaw", "threads", "default", "user-data", "workspace")
	require.NoError(t, os.MkdirAll(workspace, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(workspace, "subdir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "file1.txt"), []byte("test"), 0o644))

	// 设置sandbox provider
	provider := localsandbox.NewLocalSandboxProvider(
		sandbox.SandboxConfig{Type: sandbox.SandboxTypeLocal, WorkDir: ".goclaw"},
		".goclaw",
		"",
	)
	sandbox.SetDefaultProvider(provider)
	t.Cleanup(func() { sandbox.SetDefaultProvider(nil) })

	// 注册默认工具
	cfg := &config.AppConfig{}
	require.NoError(t, RegisterDefaultTools(cfg))

	// 获取list_dir工具
	listTool, ok := tools.Get("list_dir")
	require.True(t, ok, "list_dir tool should be registered")

	// 执行列出
	result, err := listTool.Execute(context.Background(), `{"description":"test","path":"/mnt/user-data/workspace"}`)
	require.NoError(t, err)
	assert.Contains(t, result, "file1.txt")
	assert.Contains(t, result, "subdir/")
}

func TestBashRuntimeTool_Disabled(t *testing.T) {
	// 重置默认注册表
	tools.ResetDefaultRegistry()

	// 测试bash被禁用的配置
	cfg := &config.AppConfig{
		Sandbox: config.SandboxConfig{
			Use:           "local",
			AllowHostBash: false,
		},
	}

	err := RegisterDefaultTools(cfg)
	require.NoError(t, err)

	// 获取bash工具
	bashTool, ok := tools.Get("bash")
	require.True(t, ok, "bash tool should be registered")

	// 执行bash命令应该返回禁用错误
	result, err := bashTool.Execute(context.Background(), `{"description":"test","command":"echo hello"}`)
	require.NoError(t, err)
	assert.Contains(t, result, "disabled", "expected bash to be disabled")
}

func TestBashRuntimeTool_Enabled(t *testing.T) {
	// 保存当前目录
	cwd, _ := os.Getwd()
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	// 创建workspace目录
	workspace := filepath.Join(".goclaw", "threads", "default", "user-data", "workspace")
	require.NoError(t, os.MkdirAll(workspace, 0o755))

	// 重置默认注册表
	tools.ResetDefaultRegistry()

	// 测试bash被启用的配置
	cfg := &config.AppConfig{
		Sandbox: config.SandboxConfig{
			Use:           "docker", // docker模式启用bash
			AllowHostBash: false,
		},
	}

	err := RegisterDefaultTools(cfg)
	require.NoError(t, err)

	// 获取bash工具
	bashTool, ok := tools.Get("bash")
	require.True(t, ok, "bash tool should be registered")

	// bash工具的输入schema应该正确
	schema := bashTool.InputSchema()
	assert.NotNil(t, schema)
	assert.True(t, len(schema) > 0)
}

func TestToolIntExtra(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.AppConfig
		toolName string
		key      string
		fallback int
		want     int
	}{
		{
			name:     "nil config",
			cfg:      nil,
			toolName: "test",
			key:      "max_results",
			fallback: 10,
			want:     10,
		},
		{
			name: "int value",
			cfg: &config.AppConfig{
				Tools: []config.ToolConfig{
					{Name: "test", Extra: map[string]any{"max_results": 20}},
				},
			},
			toolName: "test",
			key:      "max_results",
			fallback: 10,
			want:     20,
		},
		{
			name: "int64 value",
			cfg: &config.AppConfig{
				Tools: []config.ToolConfig{
					{Name: "test", Extra: map[string]any{"max_results": int64(30)}},
				},
			},
			toolName: "test",
			key:      "max_results",
			fallback: 10,
			want:     30,
		},
		{
			name: "float64 value",
			cfg: &config.AppConfig{
				Tools: []config.ToolConfig{
					{Name: "test", Extra: map[string]any{"max_results": float64(40)}},
				},
			},
			toolName: "test",
			key:      "max_results",
			fallback: 10,
			want:     40,
		},
		{
			name: "string value",
			cfg: &config.AppConfig{
				Tools: []config.ToolConfig{
					{Name: "test", Extra: map[string]any{"max_results": "50"}},
				},
			},
			toolName: "test",
			key:      "max_results",
			fallback: 10,
			want:     50,
		},
		{
			name: "invalid string value",
			cfg: &config.AppConfig{
				Tools: []config.ToolConfig{
					{Name: "test", Extra: map[string]any{"max_results": "invalid"}},
				},
			},
			toolName: "test",
			key:      "max_results",
			fallback: 10,
			want:     10,
		},
		{
			name: "missing key",
			cfg: &config.AppConfig{
				Tools: []config.ToolConfig{
					{Name: "test", Extra: map[string]any{"other_key": 100}},
				},
			},
			toolName: "test",
			key:      "max_results",
			fallback: 10,
			want:     10,
		},
		{
			name: "missing tool config",
			cfg: &config.AppConfig{
				Tools: []config.ToolConfig{},
			},
			toolName: "nonexistent",
			key:      "max_results",
			fallback: 10,
			want:     10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolIntExtra(tt.cfg, tt.toolName, tt.key, tt.fallback)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReplaceVirtualPaths(t *testing.T) {
	paths := &fstools.PathMapping{
		WorkspacePath: "/host/workspace",
		UploadsPath:   "/host/uploads",
		OutputsPath:   "/host/outputs",
	}

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "cat /mnt/user-data/workspace/file.txt",
			expected: "cat /host/workspace/file.txt",
		},
		{
			input:    "ls /mnt/user-data/uploads/",
			expected: "ls /host/uploads/",
		},
		{
			input:    "echo > /mnt/user-data/outputs/result.json",
			expected: "echo > /host/outputs/result.json",
		},
		{
			input:    "cat /some/other/path",
			expected: "cat /some/other/path",
		},
		{
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := replaceVirtualPaths(tt.input, paths)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestReplaceVirtualPaths_NilPaths(t *testing.T) {
	input := "cat /mnt/user-data/workspace/file.txt"
	got := replaceVirtualPaths(input, nil)
	// 当paths为nil时，应该返回原始命令
	assert.Equal(t, input, got)
}

func TestRuntimePathResolver_Resolve(t *testing.T) {
	paths := &fstools.PathMapping{
		WorkspacePath: "/host/workspace",
		UploadsPath:   "/host/uploads",
		OutputsPath:   "/host/outputs",
	}

	tests := []struct {
		name        string
		virtualPath string
		want        string
		wantErr     bool
	}{
		{
			name:        "workspace path",
			virtualPath: "/mnt/user-data/workspace/file.txt",
			want:        filepath.Join("/host/workspace", "file.txt"),
			wantErr:     false,
		},
		{
			name:        "uploads path",
			virtualPath: "/mnt/user-data/uploads/image.png",
			want:        filepath.Join("/host/uploads", "image.png"),
			wantErr:     false,
		},
		{
			name:        "outputs path",
			virtualPath: "/mnt/user-data/outputs/result.json",
			want:        filepath.Join("/host/outputs", "result.json"),
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &runtimePathResolver{paths: paths}
			got, err := resolver.Resolve(tt.virtualPath)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestRuntimePathResolver_Resolve_Nil(t *testing.T) {
	// 测试nil resolver
	var resolver *runtimePathResolver
	_, err := resolver.Resolve("/mnt/user-data/workspace/test.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestRuntimePathResolver_MaskHostPaths(t *testing.T) {
	paths := &fstools.PathMapping{
		WorkspacePath: "/host/workspace",
		UploadsPath:   "/host/uploads",
		OutputsPath:   "/host/outputs",
	}

	resolver := &runtimePathResolver{paths: paths}

	input := "File at /host/workspace/test.txt and /host/uploads/img.png"
	got := resolver.MaskHostPaths(input)
	assert.Contains(t, got, "/mnt/user-data/workspace")
	assert.Contains(t, got, "/mnt/user-data/uploads")
	assert.NotContains(t, got, "/host/workspace")
	assert.NotContains(t, got, "/host/uploads")
}

func TestRuntimePathResolver_MaskHostPaths_Nil(t *testing.T) {
	// 测试nil resolver
	var resolver *runtimePathResolver
	input := "File at /host/workspace/test.txt"
	got := resolver.MaskHostPaths(input)
	// 当resolver为nil时，应该返回原始字符串
	assert.Equal(t, input, got)
}

func TestRuntimePathResolver_MaskHostPaths_EmptyPaths(t *testing.T) {
	// 测试空的path mapping
	resolver := &runtimePathResolver{paths: &fstools.PathMapping{}}
	input := "File at /host/workspace/test.txt"
	got := resolver.MaskHostPaths(input)
	// 当paths为空时，应该返回原始字符串
	assert.Equal(t, input, got)
}

func TestRegisterDefaultTools_DuplicateRegistration(t *testing.T) {
	// 重置默认注册表
	tools.ResetDefaultRegistry()

	cfg := &config.AppConfig{}

	// 第一次注册应该成功
	err := RegisterDefaultTools(cfg)
	require.NoError(t, err)

	// 验证工具已注册
	_, ok := tools.Get("read_file")
	assert.True(t, ok)

	// 重置并再次注册
	tools.ResetDefaultRegistry()
	err = RegisterDefaultTools(cfg)
	require.NoError(t, err)
}

func TestRegisterDefaultTools_BuiltinTools(t *testing.T) {
	// 重置默认注册表
	tools.ResetDefaultRegistry()

	cfg := &config.AppConfig{}
	err := RegisterDefaultTools(cfg)
	require.NoError(t, err)

	// 验证所有builtin工具都已注册
	builtinTools := []string{
		"ask_clarification",
		"tool_search",
		"task",
		"setup_agent",
	}

	for _, name := range builtinTools {
		tool, ok := tools.Get(name)
		assert.True(t, ok, "expected builtin tool %q to be registered", name)
		if ok {
			// 验证工具实现了正确的接口
			assert.NotEmpty(t, tool.Name())
			assert.NotEmpty(t, tool.Description())
			assert.NotNil(t, tool.InputSchema())
		}
	}
}

func TestClarificationTool_ThroughRegistry(t *testing.T) {
	// 重置默认注册表
	tools.ResetDefaultRegistry()

	cfg := &config.AppConfig{}
	err := RegisterDefaultTools(cfg)
	require.NoError(t, err)

	// 获取clarification工具
	clarTool, ok := tools.Get("ask_clarification")
	require.True(t, ok, "ask_clarification tool should be registered")

	// 测试执行
	result, err := clarTool.Execute(context.Background(), `{"description":"test","question":"What is your name?"}`)
	require.NoError(t, err)
	assert.Contains(t, result, "question")
	assert.Contains(t, result, "What is your name?")
}

func TestToolSearchTool_ThroughRegistry(t *testing.T) {
	// 重置默认注册表
	tools.ResetDefaultRegistry()

	cfg := &config.AppConfig{}
	err := RegisterDefaultTools(cfg)
	require.NoError(t, err)

	// 获取tool_search工具
	searchTool, ok := tools.Get("tool_search")
	require.True(t, ok, "tool_search tool should be registered")

	// 测试搜索
	result, err := searchTool.Execute(context.Background(), `{"query":"file"}`)
	require.NoError(t, err)
	assert.Contains(t, result, "query")
	assert.Contains(t, result, "count")
}

func TestTaskTool_ThroughRegistry(t *testing.T) {
	// 重置默认注册表
	tools.ResetDefaultRegistry()

	cfg := &config.AppConfig{}
	err := RegisterDefaultTools(cfg)
	require.NoError(t, err)

	// 获取task工具
	taskTool, ok := tools.Get("task")
	require.True(t, ok, "task tool should be registered")

	// 验证工具基本信息
	assert.Equal(t, "task", taskTool.Name())
	assert.NotEmpty(t, taskTool.Description())
	assert.NotNil(t, taskTool.InputSchema())
}

func TestSetupAgentTool_ThroughRegistry(t *testing.T) {
	// 重置默认注册表
	tools.ResetDefaultRegistry()

	cfg := &config.AppConfig{}
	err := RegisterDefaultTools(cfg)
	require.NoError(t, err)

	// 获取setup_agent工具
	setupTool, ok := tools.Get("setup_agent")
	require.True(t, ok, "setup_agent tool should be registered")

	// 验证工具基本信息
	assert.Equal(t, "setup_agent", setupTool.Name())
	assert.NotEmpty(t, setupTool.Description())
	assert.NotNil(t, setupTool.InputSchema())
}

// mockToolRegistry 用于测试的工具注册表
type mockToolRegistry struct {
	tools []builtin.Tool
}

func (m *mockToolRegistry) Register(t builtin.Tool) error {
	m.tools = append(m.tools, t)
	return nil
}

func TestRegisterDefaultTools_WebTools(t *testing.T) {
	// 重置默认注册表
	tools.ResetDefaultRegistry()

	cfg := &config.AppConfig{}
	err := RegisterDefaultTools(cfg)
	require.NoError(t, err)

	// 验证web工具已注册
	webTools := []string{
		"web_search",
		"web_fetch",
		"image_search",
	}

	for _, name := range webTools {
		tool, ok := tools.Get(name)
		assert.True(t, ok, "expected web tool %q to be registered", name)
		if ok {
			assert.NotEmpty(t, tool.Name())
			assert.NotEmpty(t, tool.Description())
		}
	}
}

func TestSandboxSearchAdapter_Glob(t *testing.T) {
	// 测试nil sandbox的情况
	adapter := sandboxSearchAdapter{sb: nil}
	_, _, err := adapter.Glob(context.Background(), "/", "*.txt", false, 100)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}

func TestSandboxSearchAdapter_Grep(t *testing.T) {
	// 测试nil sandbox的情况
	adapter := sandboxSearchAdapter{sb: nil}
	_, _, err := adapter.Grep(context.Background(), "/", "pattern", "*.go", true, true, 100)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}
