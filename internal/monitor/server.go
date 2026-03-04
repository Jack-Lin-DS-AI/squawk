package monitor

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jack-Lin-DS-AI/squawk/internal/rules"
	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
)

// RuleEvaluator evaluates supervision rules against recent activities and
// the current event.
type RuleEvaluator interface {
	Evaluate(activities []types.Activity, event types.Event) []types.RuleMatch
	ReplaceRules(newRules []types.Rule)
}

// ActionExecutor executes the action associated with a rule match and returns
// the appropriate hook response. For side-effect-only actions (notify, log) the
// returned response may be nil.
type ActionExecutor interface {
	Execute(match types.RuleMatch) (*types.HookResponse, error)
}

// Server is an HTTP server that receives Claude Code hook events, tracks
// activities, and evaluates supervision rules.
type Server struct {
	addr       string
	rulesDir   string
	tracker    *Tracker
	evaluator  RuleEvaluator
	executor   ActionExecutor
	adminToken string // shared secret for admin endpoints
	mux        *http.ServeMux
}

// NewServer creates a new hook server bound to the given address. The executor
// is optional; when nil the server falls back to built-in behavior (log-only
// for PostToolUse, inline block for PreToolUse). The rulesDir is used for
// hot-reloading rules via the admin API. The adminToken, when non-empty,
// requires Bearer token authentication on admin endpoints.
func NewServer(addr, rulesDir string, tracker *Tracker, evaluator RuleEvaluator, executor ActionExecutor, adminToken string) *Server {
	s := &Server{
		addr:       addr,
		rulesDir:   rulesDir,
		tracker:    tracker,
		evaluator:  evaluator,
		executor:   executor,
		adminToken: adminToken,
		mux:        http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

// ServeHTTP implements http.Handler so the server can be used with httptest.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// ListenAndServe starts the HTTP server. It blocks until the server exits.
func (s *Server) ListenAndServe() error {
	srv := &http.Server{
		Addr:         s.addr,
		Handler:      s.mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  30 * time.Second,
	}
	return srv.ListenAndServe()
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /status", s.handleStatus)
	s.mux.HandleFunc("POST /hooks/pre-tool-use", s.handlePreToolUse)
	s.mux.HandleFunc("POST /hooks/post-tool-use", s.handlePostToolUse)
	s.mux.HandleFunc("POST /hooks/event", s.handleEvent)
	s.mux.HandleFunc("POST /admin/reload-rules", s.handleReloadRules)
}

// handleHealth responds with a simple health check.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// statusResponse is the JSON shape returned by the /status endpoint.
type statusResponse struct {
	Sessions map[string]int `json:"sessions"`
}

// handleStatus returns tracked sessions and their activity counts.
func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, statusResponse{Sessions: s.tracker.SessionCounts()})
}

// decodeEvent decodes and validates the event from the request body.
// Returns false if decoding failed (error response already written).
func decodeEvent(w http.ResponseWriter, r *http.Request) (types.Event, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit
	var event types.Event
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "failed to decode request body: " + err.Error(),
		})
		return event, false
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	return event, true
}

// blockResponse builds a default block HookResponse from the rule's action message.
func blockResponse(action types.Action) types.HookResponse {
	return types.HookResponse{Decision: "block", Reason: action.Message}
}

// handlePreToolUse handles PreToolUse hook events. If a matching rule has an
// ActionBlock action, the response instructs Claude Code to block the tool call.
func (s *Server) handlePreToolUse(w http.ResponseWriter, r *http.Request) {
	event, ok := decodeEvent(w, r)
	if !ok {
		return
	}

	activities := s.tracker.GetActivities(event.SessionID)
	matches := s.evaluator.Evaluate(activities, event)

	// Check if any match requires blocking the current event.
	for _, m := range matches {
		if m.Rule.Action.Type != types.ActionBlock || !eventInScope(event, m.Rule.Action) {
			continue
		}
		if s.executor != nil {
			resp, err := s.executor.Execute(m)
			if err != nil {
				log.Printf("executor error for rule %q: %v", m.Rule.Name, err)
			} else if resp != nil {
				writeJSON(w, http.StatusOK, *resp)
				return
			}
		}
		writeJSON(w, http.StatusOK, blockResponse(m.Rule.Action))
		return
	}

	// No blocking rules matched — allow.
	writeJSON(w, http.StatusOK, types.HookResponse{})
}

// handlePostToolUse handles PostToolUse hook events. It records the activity
// and evaluates rules. Matches are logged but do not block.
func (s *Server) handlePostToolUse(w http.ResponseWriter, r *http.Request) {
	event, ok := decodeEvent(w, r)
	if !ok {
		return
	}

	s.tracker.Record(event)

	activities := s.tracker.GetActivities(event.SessionID)
	matches := s.evaluator.Evaluate(activities, event)

	var injectResp *types.HookResponse
	for _, m := range matches {
		if s.executor != nil {
			resp, err := s.executor.Execute(m)
			if err != nil {
				log.Printf("executor error for rule %q: %v", m.Rule.Name, err)
				continue
			}
			// Capture the first inject response (non-nil with AdditionalContext).
			if injectResp == nil && resp != nil && resp.AdditionalContext != "" {
				injectResp = resp
			}
		} else {
			log.Printf("rule matched: %s (action=%s, session=%s)",
				m.Rule.Name, m.Rule.Action.Type, event.SessionID)
		}
	}

	if injectResp != nil {
		writeJSON(w, http.StatusOK, *injectResp)
		return
	}
	writeJSON(w, http.StatusOK, types.HookResponse{})
}

// handleEvent handles generic hook events (Notification, etc.). It records
// the activity without evaluation.
func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	event, ok := decodeEvent(w, r)
	if !ok {
		return
	}

	s.tracker.Record(event)
	writeJSON(w, http.StatusOK, types.HookResponse{})
}

// eventInScope checks whether the current PreToolUse event falls within the
// block action's scope. If no scope is defined, all events are in scope.
func eventInScope(event types.Event, action types.Action) bool {
	// Check tool scope using pre-compiled regex.
	if action.ToolScopeRe != nil {
		if !action.ToolScopeRe.MatchString(event.ToolName) {
			return false
		}
	}

	// Check file scope.
	if action.FileScope != "" {
		filePath, _ := event.ToolInput["file_path"].(string)
		if filePath == "" {
			return false
		}
		baseName := filepath.Base(filePath)
		matched := false
		for _, pattern := range strings.Split(action.FileScope, "|") {
			if ok, _ := filepath.Match(strings.TrimSpace(pattern), baseName); ok {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

// handleReloadRules reloads rules from disk and replaces them in the engine.
func (s *Server) handleReloadRules(w http.ResponseWriter, r *http.Request) {
	if s.adminToken != "" {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+s.adminToken {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "unauthorized",
			})
			return
		}
	}

	if s.rulesDir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "rules directory not configured",
		})
		return
	}

	loadedRules, err := rules.LoadRules(s.rulesDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("failed to reload rules: %v", err),
		})
		return
	}

	s.evaluator.ReplaceRules(loadedRules)

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "reloaded",
		"count":  len(loadedRules),
	})
}

// writeJSON encodes v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("failed to write JSON response: %v", err)
	}
}
