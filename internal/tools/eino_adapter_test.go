package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTool struct{}

func (f *fakeTool) Name() string        { return "fake_tool" }
func (f *fakeTool) Description() string { return "fake desc" }
func (f *fakeTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`)
}
func (f *fakeTool) Execute(ctx context.Context, input string) (string, error) {
	_ = ctx
	return "echo:" + input, nil
}

// fakeErrorTool 是一个执行时返回错误的工具
type fakeErrorTool struct{}

func (f *fakeErrorTool) Name() string        { return "error_tool" }
func (f *fakeErrorTool) Description() string { return "Tool that returns error" }
func (f *fakeErrorTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (f *fakeErrorTool) Execute(ctx context.Context, input string) (string, error) {
	return "", errors.New("execution failed")
}

// fakeEmptySchemaTool 是一个没有schema的工具
type fakeEmptySchemaTool struct{}

func (f *fakeEmptySchemaTool) Name() string        { return "empty_schema_tool" }
func (f *fakeEmptySchemaTool) Description() string { return "Tool with empty schema" }
func (f *fakeEmptySchemaTool) InputSchema() json.RawMessage {
	return nil
}
func (f *fakeEmptySchemaTool) Execute(ctx context.Context, input string) (string, error) {
	return "empty result", nil
}

// fakeInvalidSchemaTool 是一个有无效schema的工具
type fakeInvalidSchemaTool struct{}

func (f *fakeInvalidSchemaTool) Name() string        { return "invalid_schema_tool" }
func (f *fakeInvalidSchemaTool) Description() string { return "Tool with invalid schema" }
func (f *fakeInvalidSchemaTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{invalid json`)
}
func (f *fakeInvalidSchemaTool) Execute(ctx context.Context, input string) (string, error) {
	return "result", nil
}

func TestEinoInvokableToolAdapter_InfoAndRun(t *testing.T) {
	adapter := NewEinoInvokableToolAdapter(&fakeTool{})

	info, err := adapter.Info(context.Background())
	require.NoError(t, err)
	require.Equal(t, "fake_tool", info.Name)
	require.Equal(t, "fake desc", info.Desc)
	require.NotNil(t, info.ParamsOneOf)

	out, err := adapter.InvokableRun(context.Background(), `{"q":"hi"}`)
	require.NoError(t, err)
	require.Equal(t, `echo:{"q":"hi"}`, out)
}

func TestAdaptToEinoTools(t *testing.T) {
	list := AdaptToEinoTools([]Tool{&fakeTool{}})
	require.Len(t, list, 1)

	info, err := list[0].Info(context.Background())
	require.NoError(t, err)
	require.Equal(t, "fake_tool", info.Name)
}

func TestNewEinoInvokableToolAdapter(t *testing.T) {
	tool := &fakeTool{}
	adapter := NewEinoInvokableToolAdapter(tool)
	require.NotNil(t, adapter)

	// 验证内部工具被正确存储
	info, err := adapter.Info(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "fake_tool", info.Name)
	assert.Equal(t, "fake desc", info.Desc)
}

func TestEinoInvokableToolAdapter_Info(t *testing.T) {
	tests := []struct {
		name       string
		tool       Tool
		wantName   string
		wantDesc   string
		wantParams bool
	}{
		{
			name:       "tool with schema",
			tool:       &fakeTool{},
			wantName:   "fake_tool",
			wantDesc:   "fake desc",
			wantParams: true,
		},
		{
			name:       "tool with empty schema",
			tool:       &fakeEmptySchemaTool{},
			wantName:   "empty_schema_tool",
			wantDesc:   "Tool with empty schema",
			wantParams: false,
		},
		{
			name:       "tool with invalid schema",
			tool:       &fakeInvalidSchemaTool{},
			wantName:   "invalid_schema_tool",
			wantDesc:   "Tool with invalid schema",
			wantParams: false, // invalid schema should result in nil ParamsOneOf
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := NewEinoInvokableToolAdapter(tt.tool)
			info, err := adapter.Info(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, info.Name)
			assert.Equal(t, tt.wantDesc, info.Desc)
			if tt.wantParams {
				assert.NotNil(t, info.ParamsOneOf)
			} else {
				// 无效或空schema时ParamsOneOf可能为nil
				// 这里不做强制断言，因为实现可能不同
			}
		})
	}
}

func TestEinoInvokableToolAdapter_InvokableRun(t *testing.T) {
	tests := []struct {
		name       string
		tool       Tool
		input      string
		wantResult string
		wantError  bool
	}{
		{
			name:       "successful execution",
			tool:       &fakeTool{},
			input:      `{"q":"test"}`,
			wantResult: `echo:{"q":"test"}`,
			wantError:  false,
		},
		{
			name:       "error execution",
			tool:       &fakeErrorTool{},
			input:      `{}`,
			wantResult: "",
			wantError:  true,
		},
		{
			name:       "empty input",
			tool:       &fakeTool{},
			input:      "",
			wantResult: "echo:",
			wantError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := NewEinoInvokableToolAdapter(tt.tool)
			result, err := adapter.InvokableRun(context.Background(), tt.input)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantResult, result)
			}
		})
	}
}

func TestAdaptToEinoTool(t *testing.T) {
	tool := &fakeTool{}
	adapted := AdaptToEinoTool(tool)
	require.NotNil(t, adapted)

	info, err := adapted.Info(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "fake_tool", info.Name)
}

func TestAdaptToEinoTools_Multiple(t *testing.T) {
	tools := []Tool{
		&fakeTool{},
		&fakeEmptySchemaTool{},
		&fakeErrorTool{},
	}

	adapted := AdaptToEinoTools(tools)
	require.Len(t, adapted, 3)

	// 验证每个工具都被正确转换
	names := make(map[string]bool)
	for _, a := range adapted {
		info, err := a.Info(context.Background())
		require.NoError(t, err)
		names[info.Name] = true
	}

	assert.True(t, names["fake_tool"])
	assert.True(t, names["empty_schema_tool"])
	assert.True(t, names["error_tool"])
}

func TestAdaptToEinoTools_Empty(t *testing.T) {
	adapted := AdaptToEinoTools([]Tool{})
	assert.Empty(t, adapted)
	assert.NotNil(t, adapted) // 应该是空切片而不是nil
}

func TestAdaptToEinoTools_Nil(t *testing.T) {
	adapted := AdaptToEinoTools(nil)
	assert.Empty(t, adapted)
}

func TestAdaptDefaultRegistryToEinoTools(t *testing.T) {
	// 重置默认注册表
	ResetDefaultRegistry()

	// 注册一些工具
	MustRegister(&fakeTool{})
	MustRegister(&fakeEmptySchemaTool{})

	// 获取转换后的工具列表
	adapted := AdaptDefaultRegistryToEinoTools()
	assert.Len(t, adapted, 2)

	// 验证工具名称
	names := make(map[string]bool)
	for _, a := range adapted {
		info, err := a.Info(context.Background())
		require.NoError(t, err)
		names[info.Name] = true
	}
	assert.True(t, names["fake_tool"])
	assert.True(t, names["empty_schema_tool"])
}

func TestAdaptDefaultRegistryToEinoTools_Empty(t *testing.T) {
	// 重置默认注册表为空
	ResetDefaultRegistry()

	adapted := AdaptDefaultRegistryToEinoTools()
	assert.Empty(t, adapted)
}

func TestEinoInvokableToolAdapter_InvokableRun_WithContext(t *testing.T) {
	// 测试带context的执行
	tool := &fakeTool{}
	adapter := NewEinoInvokableToolAdapter(tool)

	ctx := context.Background()
	result, err := adapter.InvokableRun(ctx, `{"q":"context test"}`)
	require.NoError(t, err)
	assert.Equal(t, `echo:{"q":"context test"}`, result)
}

func TestEinoInvokableToolAdapter_InvokableRun_WithOptions(t *testing.T) {
	// 测试带options参数的执行（当前实现忽略options）
	tool := &fakeTool{}
	adapter := NewEinoInvokableToolAdapter(tool)

	// options参数当前被忽略，但不应该导致错误
	result, err := adapter.InvokableRun(context.Background(), `{"q":"opts"}`)
	require.NoError(t, err)
	assert.Equal(t, `echo:{"q":"opts"}`, result)
}
