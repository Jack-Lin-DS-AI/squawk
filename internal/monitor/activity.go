// Package monitor provides an HTTP hook server and activity tracker for
// receiving Claude Code hook events and evaluating supervision rules.
package monitor

import (
	"sync"
	"time"

	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
)

const (
	maxSessions             = 1000
	maxActivitiesPerSession = 500
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

	// Cap total sessions: if at limit, evict oldest session.
	if _, exists := t.sessions[event.SessionID]; !exists && len(t.sessions) >= maxSessions {
		t.evictOldestLocked()
	}

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

// SessionCounts returns the number of tracked activities per session.
func (t *Tracker) SessionCounts() map[string]int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	counts := make(map[string]int, len(t.sessions))
	for sid, acts := range t.sessions {
		counts[sid] = len(acts)
	}
	return counts
}

// evictOldestLocked removes the session whose most recent activity is the
// oldest across all sessions. Must be called with t.mu held for writing.
func (t *Tracker) evictOldestLocked() {
	var oldestSID string
	var oldestTime time.Time
	for sid, acts := range t.sessions {
		if len(acts) > 0 {
			last := acts[len(acts)-1].Timestamp
			if oldestSID == "" || last.Before(oldestTime) {
				oldestSID = sid
				oldestTime = last
			}
		}
	}
	if oldestSID != "" {
		delete(t.sessions, oldestSID)
	}
}

// cleanupLocked removes activities older than the window for a given session
// and enforces the per-session activity cap. Must be called with t.mu held
// for writing.
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
		// Copy to new slice to release underlying array memory.
		trimmed := make([]types.Activity, len(activities)-firstValid)
		copy(trimmed, activities[firstValid:])
		activities = trimmed
	}

	// Cap per-session activities.
	if len(activities) > maxActivitiesPerSession {
		activities = activities[len(activities)-maxActivitiesPerSession:]
	}
	t.sessions[sessionID] = activities
}
