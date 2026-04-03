package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeTool struct{}

func (f *fakeTool) Name() string { return "fake_tool" }
func (f *fakeTool) Description() string { return "fake desc" }
func (f *fakeTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`)
}
func (f *fakeTool) Execute(ctx context.Context, input string) (string, error) {
	_ = ctx
	return "echo:" + input, nil
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
