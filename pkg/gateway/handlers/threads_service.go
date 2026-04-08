package handlers

import (
	"context"
	"fmt"
	"sync"

	"github.com/bookerbai/goclaw/internal/agent"
	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/internal/threadstore"
)

// runHandle tracks an in-progress run for cancellation and lookup.
type runHandle struct {
	RunID        string
	ThreadID     string
	CheckpointID string
	Cancel       context.CancelFunc
}

// ThreadsService handles the business logic for thread operations.
type ThreadsService struct {
	cfg   *config.AppConfig
	agent agent.LeadAgent
	store threadstore.Store

	runsMu sync.RWMutex
	runs   map[string]runHandle
}

// NewThreadsService creates a service wired to the given agent.
func NewThreadsService(cfg *config.AppConfig, a agent.LeadAgent, store threadstore.Store) *ThreadsService {
	if store == nil {
		// Create default file store
		var err error
		store, err = threadstore.NewFileStore("")
		if err != nil {
			panic(fmt.Sprintf("failed to create thread store: %v", err))
		}
	}
	return &ThreadsService{cfg: cfg, agent: a, store: store, runs: make(map[string]runHandle)}
}

// RegisterRun registers a new run for tracking.
func (s *ThreadsService) RegisterRun(runID string, threadID, checkpointID string, cancel context.CancelFunc) {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	s.runs[runID] = runHandle{
		RunID:        runID,
		ThreadID:     threadID,
		CheckpointID: checkpointID,
		Cancel:       cancel,
	}
}

// UnregisterRun removes a run from tracking.
func (s *ThreadsService) UnregisterRun(runID string) {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	delete(s.runs, runID)
}

// GetRun retrieves a run by ID.
func (s *ThreadsService) GetRun(runID string) (runHandle, bool) {
	s.runsMu.RLock()
	defer s.runsMu.RUnlock()
	r, ok := s.runs[runID]
	return r, ok
}

// ListRunsForThread returns all runs for a specific thread.
// Returns a map of runID to runHandle for easy access to both.
func (s *ThreadsService) ListRunsForThread(threadID string) map[string]runHandle {
	s.runsMu.RLock()
	defer s.runsMu.RUnlock()

	result := make(map[string]runHandle)
	for runID, rh := range s.runs {
		if rh.ThreadID == threadID {
			result[runID] = rh
		}
	}
	return result
}

// GetRunCount returns the total number of tracked runs.
func (s *ThreadsService) GetRunCount() int {
	s.runsMu.RLock()
	defer s.runsMu.RUnlock()
	return len(s.runs)
}

// GetStore returns the thread store.
func (s *ThreadsService) GetStore() threadstore.Store {
	return s.store
}

// GetAgent returns the lead agent.
func (s *ThreadsService) GetAgent() agent.LeadAgent {
	return s.agent
}

// GetConfig returns the app config.
func (s *ThreadsService) GetConfig() *config.AppConfig {
	return s.cfg
}
