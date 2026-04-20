package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"

	"goclaw/pkg/metrics"
)

// EinoInvokableToolAdapter 将 GoClaw Tool 适配为 Eino InvokableTool。
type EinoInvokableToolAdapter struct {
	inner Tool
}

// NewEinoInvokableToolAdapter 创建单个工具适配器。
func NewEinoInvokableToolAdapter(inner Tool) *EinoInvokableToolAdapter {
	return &EinoInvokableToolAdapter{inner: inner}
}

// Info 返回 Eino 所需工具元信息。
func (a *EinoInvokableToolAdapter) Info(ctx context.Context) (*schema.ToolInfo, error) {
	_ = ctx
	info := &schema.ToolInfo{
		Name: a.inner.Name(),
		Desc: a.inner.Description(),
	}

	raw := a.inner.InputSchema()
	if len(raw) == 0 {
		return info, nil
	}

	var js jsonschema.Schema
	if err := json.Unmarshal(raw, &js); err == nil {
		info.ParamsOneOf = schema.NewParamsOneOfByJSONSchema(&js)
	}
	return info, nil
}

// InvokableRun 透传到原始 Tool.Execute，并记录指标。
func (a *EinoInvokableToolAdapter) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	_ = opts
	start := time.Now()
	toolName := a.inner.Name()

	result, err := a.inner.Execute(ctx, argumentsInJSON)

	duration := time.Since(start)
	status := "success"
	if err != nil {
		status = "error"
	}
	metrics.RecordToolExecution(toolName, duration, status)

	return result, err
}

// AdaptToEinoTool 将单个 Tool 转换为 Eino tool.BaseTool。
func AdaptToEinoTool(t Tool) tool.BaseTool {
	return NewEinoInvokableToolAdapter(t)
}

// AdaptToEinoTools 将多个 Tool 转换为 Eino tool.BaseTool 切片。
func AdaptToEinoTools(list []Tool) []tool.BaseTool {
	out := make([]tool.BaseTool, 0, len(list))
	for _, t := range list {
		out = append(out, AdaptToEinoTool(t))
	}
	return out
}

// AdaptDefaultRegistryToEinoTools 将默认工具注册表导出为 Eino 工具列表。
func AdaptDefaultRegistryToEinoTools() []tool.BaseTool {
	return AdaptToEinoTools(GetAll())
}
