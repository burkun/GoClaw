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

func TestExecutorSubscribeAndEventDispatch(t *testing.T) {
	exec := NewExecutor(ExecutorConfig{MaxConcurrent: 1, DefaultTimeout: 500 * time.Millisecond})

	events := make([]TaskEvent, 0)
	var mu sync.Mutex

	taskID, err := exec.Submit(context.Background(), TaskRequest{Prompt: "test"}, func(ctx context.Context, req TaskRequest) (string, error) {
		_ = ctx
		_ = req
		time.Sleep(100 * time.Millisecond)
		return "done", nil
	})
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	// Subscribe to events.
	unsub := exec.Subscribe(taskID, func(ctx context.Context, id string, ev TaskEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})
	defer unsub()

	// Wait for task completion.
	res, err := exec.Wait(context.Background(), taskID)
	if err != nil {
		t.Fatalf("wait failed: %v", err)
	}
	if res.Status != StatusCompleted {
		t.Fatalf("expected completed, got %s", res.Status)
	}

	// Give event callbacks time to fire.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	numEvents := len(events)
	mu.Unlock()

	if numEvents == 0 {
		t.Fatalf("expected at least 1 event, got %d", numEvents)
	}
}

func TestExecutorUnsubscribe(t *testing.T) {
	exec := NewExecutor(ExecutorConfig{})

	called := false
	var mu sync.Mutex
	taskID, err := exec.Submit(context.Background(), TaskRequest{Prompt: "test"}, func(ctx context.Context, req TaskRequest) (string, error) {
		_ = ctx
		_ = req
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	unsub := exec.Subscribe(taskID, func(ctx context.Context, id string, ev TaskEvent) {
		mu.Lock()
		called = true
		mu.Unlock()
	})
	unsub()

	// Wait a bit for any async callback (which shouldn't fire after unsub).
	exec.Wait(context.Background(), taskID)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if called {
		t.Fatalf("expected callback not to be invoked after unsubscribe")
	}
}
