package eino

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

type fakeAgent struct{}

func (f *fakeAgent) Name(ctx context.Context) string {
	return "fake"
}

func (f *fakeAgent) Description(ctx context.Context) string {
	return "fake for tests"
}

func (f *fakeAgent) Run(ctx context.Context, input *adk.AgentInput, options ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer gen.Close()
		gen.Send(&adk.AgentEvent{
			AgentName: "fake",
			Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{Message: schema.AssistantMessage("ok", nil)}},
		})
	}()
	return iter
}

func TestNewRunner_Run(t *testing.T) {
	r, err := NewRunner(context.Background(), RunnerConfig{
		Agent:           &fakeAgent{},
		EnableStreaming: true,
	})
	require.NoError(t, err)

	stream := r.Run(context.Background(), []Message{schema.UserMessage("hello")})
	event, ok := stream.Next()
	require.True(t, ok)
	require.NotNil(t, event)
	require.Equal(t, "fake", event.AgentName)
}

func TestNewRunner_RequiresAgent(t *testing.T) {
	_, err := NewRunner(context.Background(), RunnerConfig{})
	require.Error(t, err)
}

func TestRunner_ResumeWithoutCheckpointStore(t *testing.T) {
	r, err := NewRunner(context.Background(), RunnerConfig{Agent: &fakeAgent{}})
	require.NoError(t, err)

	_, err = r.Resume(context.Background(), "cp-1")
	require.Error(t, err)
}
