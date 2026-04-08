package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecordAgentRun(t *testing.T) {
	// Reset counters for clean test
	AgentRunTotal.Reset()
	AgentRunDurationSeconds.Reset()

	// Record some agent runs
	RecordAgentRun("test_agent", 1*time.Second, "success")
	RecordAgentRun("test_agent", 2*time.Second, "error")
	RecordAgentRun("other_agent", 500*time.Millisecond, "success")

	// Verify counters using GatherAndCompare
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	// Check that the counter was incremented
	for _, mf := range mfs {
		if mf.GetName() == "agent_run_total" {
			// Success - metric exists
			return
		}
	}
	t.Errorf("agent_run_total metric not found")
}

func TestRecordToolExecution(t *testing.T) {
	// Reset counters for clean test
	ToolExecutionTotal.Reset()
	ToolExecutionDurationSeconds.Reset()

	// Record some tool executions
	RecordToolExecution("read_file", 100*time.Millisecond, "success")
	RecordToolExecution("read_file", 200*time.Millisecond, "success")
	RecordToolExecution("bash", 1500*time.Millisecond, "error")

	// Verify counters
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	// Check that the counter was incremented
	for _, mf := range mfs {
		if mf.GetName() == "tool_execution_total" {
			// Success - metric exists
			return
		}
	}
	t.Errorf("tool_execution_total metric not found")
}

func TestSetActiveThreads(t *testing.T) {
	// Set active threads
	SetActiveThreads(5)

	// Verify gauge value
	if val := testutil.ToFloat64(ActiveThreads); val != 5 {
		t.Errorf("expected active_threads=5, got %v", val)
	}
}

func TestSetQueueSize(t *testing.T) {
	// Set queue size
	SetQueueSize(10)

	// Verify gauge value
	if val := testutil.ToFloat64(QueueSize); val != 10 {
		t.Errorf("expected queue_size=10, got %v", val)
	}
}

func TestSetActiveRuns(t *testing.T) {
	// Set active runs
	SetActiveRuns(3)

	// Verify gauge value
	if val := testutil.ToFloat64(ActiveRuns); val != 3 {
		t.Errorf("expected active_runs=3, got %v", val)
	}
}
