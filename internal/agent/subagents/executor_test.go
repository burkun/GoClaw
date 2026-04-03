package subagents

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestExecutorSubmitWaitCompleted(t *testing.T) {
	exec := NewExecutor(ExecutorConfig{MaxConcurrent: 2, DefaultTimeout: time.Second})
	taskID, err := exec.Submit(context.Background(), TaskRequest{Prompt: "hello"}, func(ctx context.Context, req TaskRequest) (string, error) {
		_ = ctx
		_ = req
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	res, err := exec.Wait(context.Background(), taskID)
	if err != nil {
		t.Fatalf("wait failed: %v", err)
	}
	if res.Status != StatusCompleted {
		t.Fatalf("expected completed, got %s", res.Status)
	}
	if res.Output != "ok" {
		t.Fatalf("unexpected output: %q", res.Output)
	}
}

func TestExecutorTimeout(t *testing.T) {
	exec := NewExecutor(ExecutorConfig{MaxConcurrent: 1, DefaultTimeout: 20 * time.Millisecond})
	taskID, err := exec.Submit(context.Background(), TaskRequest{Prompt: "slow", Timeout: 20 * time.Millisecond}, func(ctx context.Context, req TaskRequest) (string, error) {
		_ = req
		select {
		case <-time.After(200 * time.Millisecond):
			return "late", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	res, err := exec.Wait(context.Background(), taskID)
	if err != nil {
		t.Fatalf("wait failed: %v", err)
	}
	if res.Status != StatusTimedOut {
		t.Fatalf("expected timed_out, got %s", res.Status)
	}
	if res.Error == "" {
		t.Fatalf("expected timeout error message")
	}
}

func TestExecutorConcurrencyLimit(t *testing.T) {
	exec := NewExecutor(ExecutorConfig{MaxConcurrent: 1, DefaultTimeout: time.Second})

	var mu sync.Mutex
	current := 0
	maxSeen := 0
	worker := func(ctx context.Context, req TaskRequest) (string, error) {
		_ = req
		mu.Lock()
		current++
		if current > maxSeen {
			maxSeen = current
		}
		mu.Unlock()

		select {
		case <-time.After(40 * time.Millisecond):
		case <-ctx.Done():
		}

		mu.Lock()
		current--
		mu.Unlock()
		return "ok", nil
	}

	id1, err := exec.Submit(context.Background(), TaskRequest{Prompt: "t1"}, worker)
	if err != nil {
		t.Fatalf("submit t1 failed: %v", err)
	}
	id2, err := exec.Submit(context.Background(), TaskRequest{Prompt: "t2"}, worker)
	if err != nil {
		t.Fatalf("submit t2 failed: %v", err)
	}

	if _, err := exec.Wait(context.Background(), id1); err != nil {
		t.Fatalf("wait t1 failed: %v", err)
	}
	if _, err := exec.Wait(context.Background(), id2); err != nil {
		t.Fatalf("wait t2 failed: %v", err)
	}

	if maxSeen != 1 {
		t.Fatalf("expected max concurrent workers=1, got %d", maxSeen)
	}
}

func TestExecutorFailed(t *testing.T) {
	exec := NewExecutor(ExecutorConfig{})
	taskID, err := exec.Submit(context.Background(), TaskRequest{Prompt: "boom"}, func(ctx context.Context, req TaskRequest) (string, error) {
		_ = ctx
		_ = req
		return "", errors.New("boom")
	})
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	res, err := exec.Wait(context.Background(), taskID)
	if err != nil {
		t.Fatalf("wait failed: %v", err)
	}
	if res.Status != StatusFailed {
		t.Fatalf("expected failed, got %s", res.Status)
	}
	if res.Error == "" {
		t.Fatalf("expected failure error message")
	}
}
