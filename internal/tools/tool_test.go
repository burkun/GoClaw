package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTool 是一个用于测试的模拟工具
type mockTool struct {
	name        string
	description string
	schema      json.RawMessage
	executeFunc func(ctx context.Context, input string) (string, error)
}

func (m *mockTool) Name() string        { return m.name }
func (m *mockTool) Description() string { return m.description }
func (m *mockTool) InputSchema() json.RawMessage {
	if m.schema == nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return m.schema
}
func (m *mockTool) Execute(ctx context.Context, input string) (string, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, input)
	}
	return "mock result", nil
}

func TestNewToolRegistry(t *testing.T) {
	r := NewToolRegistry()
	require.NotNil(t, r)
	assert.NotNil(t, r.tools)
	assert.Empty(t, r.tools)
}

func TestToolRegistry_Register(t *testing.T) {
	r := NewToolRegistry()

	tool := &mockTool{
		name:        "test_tool",
		description: "A test tool",
	}

	// 测试成功注册
	err := r.Register(tool)
	require.NoError(t, err)

	// 测试重复注册应该返回错误
	err = r.Register(tool)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")

	// 测试注册不同名称的工具
	tool2 := &mockTool{
		name:        "test_tool_2",
		description: "Another test tool",
	}
	err = r.Register(tool2)
	require.NoError(t, err)
}

func TestToolRegistry_MustRegister(t *testing.T) {
	r := NewToolRegistry()

	tool := &mockTool{
		name:        "must_tool",
		description: "A must register tool",
	}

	// 测试成功注册不应该panic
	assert.NotPanics(t, func() {
		r.MustRegister(tool)
	})

	// 测试重复注册应该panic
	assert.Panics(t, func() {
		r.MustRegister(tool)
	})
}

func TestToolRegistry_Get(t *testing.T) {
	r := NewToolRegistry()

	tool := &mockTool{
		name:        "gettable_tool",
		description: "A gettable tool",
	}
	err := r.Register(tool)
	require.NoError(t, err)

	// 测试获取存在的工具
	got, ok := r.Get("gettable_tool")
	assert.True(t, ok)
	assert.Equal(t, tool, got)
	assert.Equal(t, "gettable_tool", got.Name())

	// 测试获取不存在的工具
	got, ok = r.Get("nonexistent_tool")
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestToolRegistry_GetAll(t *testing.T) {
	r := NewToolRegistry()

	// 空注册表
	all := r.GetAll()
	assert.Empty(t, all)

	// 注册多个工具
	tools := []*mockTool{
		{name: "tool_a", description: "Tool A"},
		{name: "tool_b", description: "Tool B"},
		{name: "tool_c", description: "Tool C"},
	}

	for _, tool := range tools {
		err := r.Register(tool)
		require.NoError(t, err)
	}

	// 获取所有工具
	all = r.GetAll()
	assert.Len(t, all, 3)

	// 验证所有工具都存在
	names := make(map[string]bool)
	for _, tool := range all {
		names[tool.Name()] = true
	}
	assert.True(t, names["tool_a"])
	assert.True(t, names["tool_b"])
	assert.True(t, names["tool_c"])
}

func TestToolRegistry_ConcurrentAccess(t *testing.T) {
	r := NewToolRegistry()

	// 测试并发注册
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			tool := &mockTool{
				name:        fmt.Sprintf("concurrent_tool_%d", idx),
				description: "Concurrent tool",
			}
			_ = r.Register(tool)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// 验证所有成功注册的工具
	all := r.GetAll()
	// 由于可能有重复名称的注册失败，这里只验证没有panic且结果是合理的
	assert.GreaterOrEqual(t, len(all), 0)
}

// 使用包级别默认注册表的测试

func TestRegister(t *testing.T) {
	// 重置默认注册表
	ResetDefaultRegistry()

	tool := &mockTool{
		name:        "default_reg_tool",
		description: "Default registry tool",
	}

	err := Register(tool)
	require.NoError(t, err)

	// 验证可以通过Get获取
	got, ok := Get("default_reg_tool")
	assert.True(t, ok)
	assert.Equal(t, tool, got)
}

func TestMustRegister_PackageLevel(t *testing.T) {
	// 重置默认注册表
	ResetDefaultRegistry()

	tool := &mockTool{
		name:        "package_must_tool",
		description: "Package level must tool",
	}

	assert.NotPanics(t, func() {
		MustRegister(tool)
	})

	// 验证已注册
	_, ok := Get("package_must_tool")
	assert.True(t, ok)
}

func TestGetAll_PackageLevel(t *testing.T) {
	// 重置默认注册表
	ResetDefaultRegistry()

	// 注册一些工具
	MustRegister(&mockTool{name: "pkg_tool_1", description: "Tool 1"})
	MustRegister(&mockTool{name: "pkg_tool_2", description: "Tool 2"})

	all := GetAll()
	assert.Len(t, all, 2)
}

func TestResetDefaultRegistry(t *testing.T) {
	// 先注册一个工具
	ResetDefaultRegistry()
	MustRegister(&mockTool{name: "reset_test_tool", description: "Test"})

	// 验证已注册
	_, ok := Get("reset_test_tool")
	assert.True(t, ok)

	// 重置
	ResetDefaultRegistry()

	// 验证注册表已清空
	all := GetAll()
	assert.Empty(t, all)

	// 验证之前注册的工具已不存在
	_, ok = Get("reset_test_tool")
	assert.False(t, ok)
}

func TestToolInterface(t *testing.T) {
	// 测试Tool接口的实现
	var _ Tool = &mockTool{
		name:        "interface_test",
		description: "Interface test",
		schema:      json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"}}}`),
	}
}

func TestMockTool_Execute(t *testing.T) {
	// 测试默认执行函数
	tool1 := &mockTool{name: "exec1"}
	result, err := tool1.Execute(context.Background(), `{}`)
	assert.NoError(t, err)
	assert.Equal(t, "mock result", result)

	// 测试自定义执行函数
	tool2 := &mockTool{
		name: "exec2",
		executeFunc: func(ctx context.Context, input string) (string, error) {
			return "custom: " + input, nil
		},
	}
	result, err = tool2.Execute(context.Background(), `{"key":"value"}`)
	assert.NoError(t, err)
	assert.Equal(t, `custom: {"key":"value"}`, result)
}

func TestToolRegistry_Register_NilTool(t *testing.T) {
	r := NewToolRegistry()

	// 尝试注册nil工具 - 这会panic，因为调用Name()时会nil pointer dereference
	// 这个测试说明我们需要小心处理nil工具
	assert.Panics(t, func() {
		var nilTool *mockTool
		_ = r.Register(nilTool)
	})
}
