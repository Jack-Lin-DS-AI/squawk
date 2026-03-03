// Package monitor provides an HTTP hook server and activity tracker for
// receiving Claude Code hook events and evaluating supervision rules.
package monitor

import (
	"sync"
	"time"

	"github.com/jacklin/squawk/internal/types"
)

// Tracker maintains a sliding window of recent activities per session.
// It is safe for concurrent use.
type Tracker struct {
	mu         sync.RWMutex
	windowSize time.Duration
	sessions   map[string][]types.Activity
}

// NewTracker creates a new activity tracker that retains activities within
// the given window duration.
func NewTracker(windowSize time.Duration) *Tracker {
	return &Tracker{
		windowSize: windowSize,
		sessions:   make(map[string][]types.Activity),
	}
}

// Record records an event as an activity for its session. Old activities
// beyond the window are cleaned up on each call.
func (t *Tracker) Record(event types.Event) {
	activity := types.Activity{
		Event:     event,
		Timestamp: event.Timestamp,
		SessionID: event.SessionID,
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.sessions[event.SessionID] = append(t.sessions[event.SessionID], activity)
	t.cleanupLocked(event.SessionID)
}

// GetActivities returns all recent activities for a session within the
// configured window.
func (t *Tracker) GetActivities(sessionID string) []types.Activity {
	t.mu.RLock()
	defer t.mu.RUnlock()

	activities := t.sessions[sessionID]
	cutoff := time.Now().Add(-t.windowSize)

	var result []types.Activity
	for _, a := range activities {
		if !a.Timestamp.Before(cutoff) {
			result = append(result, a)
		}
	}
	return result
}

// GetActivitiesSince returns activities for a session that occurred at or
// after the given time.
func (t *Tracker) GetActivitiesSince(sessionID string, since time.Time) []types.Activity {
	t.mu.RLock()
	defer t.mu.RUnlock()

	activities := t.sessions[sessionID]

	var result []types.Activity
	for _, a := range activities {
		if !a.Timestamp.Before(since) {
			result = append(result, a)
		}
	}
	return result
}

// cleanupLocked removes activities older than the window for a given session.
// Must be called with t.mu held for writing.
func (t *Tracker) cleanupLocked(sessionID string) {
	activities := t.sessions[sessionID]
	cutoff := time.Now().Add(-t.windowSize)

	// Find first activity within the window using a linear scan.
	firstValid := -1
	for i, a := range activities {
		if !a.Timestamp.Before(cutoff) {
			firstValid = i
			break
		}
	}

	if firstValid < 0 {
		// All activities are expired.
		delete(t.sessions, sessionID)
		return
	}
	if firstValid > 0 {
		t.sessions[sessionID] = activities[firstValid:]
	}
}
