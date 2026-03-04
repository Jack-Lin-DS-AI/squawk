// Package stats computes and displays aggregated metrics from squawk action
// log entries, enabling users to measure the value of supervision rules.
package stats

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Jack-Lin-DS-AI/squawk/internal/action"
	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
)

// Report holds aggregated metrics computed from action log entries.
type Report struct {
	ActiveDays           int                      `json:"active_days"`
	Sessions             int                      `json:"sessions"`
	Interventions        int                      `json:"interventions"`
	EstimatedActionsSaved int                     `json:"estimated_actions_saved"`
	ByAction             map[string]int           `json:"by_action"`
	ByRule               []RuleCount              `json:"by_rule"`
	ByProject            map[string]ProjectReport `json:"by_project"`
	FileHotspots         []FileHotspot            `json:"file_hotspots"`
}

// ProjectReport holds per-project metrics.
type ProjectReport struct {
	Sessions              int `json:"sessions"`
	Interventions         int `json:"interventions"`
	EstimatedActionsSaved int `json:"estimated_actions_saved"`
}

// RuleCount pairs a rule name with its trigger count.
type RuleCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// FileHotspot pairs a file path with its intervention count.
type FileHotspot struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
}

// Compute aggregates log entries into a Report using time.Now() as the
// reference time for computing the monitoring span.
func Compute(entries []action.LogEntry) Report {
	return ComputeAt(entries, time.Now())
}

// ComputeAt aggregates log entries into a Report. The now parameter is the
// reference time used to compute ActiveDays (span from earliest entry to now).
// Entries may optionally be pre-filtered by the caller (e.g. by time window
// or project).
func ComputeAt(entries []action.LogEntry, now time.Time) Report {
	r := Report{
		ByAction:  make(map[string]int),
		ByProject: make(map[string]ProjectReport),
		ByRule:        []RuleCount{},
		FileHotspots: []FileHotspot{},
	}

	if len(entries) == 0 {
		return r
	}

	var earliest time.Time
	sessions := make(map[string]struct{})
	ruleCounts := make(map[string]int)
	fileCounts := make(map[string]int)
	projectSessions := make(map[string]map[string]struct{}) // project -> set of sessions

	for _, e := range entries {
		// Track earliest timestamp across all entries (including daemon_start).
		if earliest.IsZero() || e.Timestamp.Before(earliest) {
			earliest = e.Timestamp
		}

		// Skip daemon_start entries for intervention statistics.
		if e.Action == action.DaemonStartAction {
			continue
		}

		saved := estimateSaved(e)

		r.Interventions++
		r.ByAction[e.Action]++
		r.EstimatedActionsSaved += saved

		if e.SessionID != "" {
			sessions[e.SessionID] = struct{}{}
		}

		ruleCounts[e.RuleName]++

		if e.FilePath != "" {
			fileCounts[e.FilePath]++
		}

		if e.Project != "" {
			pr := r.ByProject[e.Project]
			pr.Interventions++
			pr.EstimatedActionsSaved += saved
			r.ByProject[e.Project] = pr

			if e.SessionID != "" {
				if projectSessions[e.Project] == nil {
					projectSessions[e.Project] = make(map[string]struct{})
				}
				projectSessions[e.Project][e.SessionID] = struct{}{}
			}
		}
	}

	// ActiveDays = calendar days from earliest entry to now, inclusive.
	if !earliest.IsZero() {
		r.ActiveDays = calendarDays(earliest, now) + 1
	}
	r.Sessions = len(sessions)

	// Populate per-project session counts.
	for proj, sessSet := range projectSessions {
		pr := r.ByProject[proj]
		pr.Sessions = len(sessSet)
		r.ByProject[proj] = pr
	}

	// Build sorted rule counts (descending).
	for name, count := range ruleCounts {
		r.ByRule = append(r.ByRule, RuleCount{Name: name, Count: count})
	}
	sort.Slice(r.ByRule, func(i, j int) bool {
		if r.ByRule[i].Count != r.ByRule[j].Count {
			return r.ByRule[i].Count > r.ByRule[j].Count
		}
		return r.ByRule[i].Name < r.ByRule[j].Name
	})

	// Build sorted file hotspots (descending).
	for path, count := range fileCounts {
		r.FileHotspots = append(r.FileHotspots, FileHotspot{Path: path, Count: count})
	}
	sort.Slice(r.FileHotspots, func(i, j int) bool {
		if r.FileHotspots[i].Count != r.FileHotspots[j].Count {
			return r.FileHotspots[i].Count > r.FileHotspots[j].Count
		}
		return r.FileHotspots[i].Path < r.FileHotspots[j].Path
	})

	return r
}

// calendarDays returns the number of calendar days between two timestamps
// using UTC dates. Times within the same UTC calendar day return 0.
func calendarDays(from, to time.Time) int {
	fy, fm, fd := from.UTC().Date()
	ty, tm, td := to.UTC().Date()
	fromDate := time.Date(fy, fm, fd, 0, 0, 0, 0, time.UTC)
	toDate := time.Date(ty, tm, td, 0, 0, 0, 0, time.UTC)
	days := int(toDate.Sub(fromDate) / (24 * time.Hour))
	if days < 0 {
		return 0
	}
	return days
}

// estimateSaved returns an estimated number of wasted actions prevented by
// this intervention.
//
//   - block: activity_count + 3 (prevents ~3 additional wasted actions)
//   - inject: activity_count / 2 (partial credit: redirected but not stopped)
//   - other: 1 (minimal credit for awareness)
func estimateSaved(e action.LogEntry) int {
	switch e.Action {
	case string(types.ActionBlock):
		saved := e.ActivityCount + 3
		if saved < 1 {
			saved = 1
		}
		return saved
	case string(types.ActionInject):
		saved := e.ActivityCount / 2
		if saved < 1 {
			saved = 1
		}
		return saved
	default:
		return 1
	}
}

// FilterSince returns only entries with timestamps at or after the given time.
func FilterSince(entries []action.LogEntry, since time.Time) []action.LogEntry {
	var result []action.LogEntry
	for _, e := range entries {
		if !e.Timestamp.Before(since) {
			result = append(result, e)
		}
	}
	return result
}

// FilterProject returns only entries whose Project field matches the given path.
// Paths are normalized with filepath.Clean before comparison.
func FilterProject(entries []action.LogEntry, project string) []action.LogEntry {
	project = filepath.Clean(project)
	var result []action.LogEntry
	for _, e := range entries {
		if filepath.Clean(e.Project) == project {
			result = append(result, e)
		}
	}
	return result
}

// PrintReport writes a human-readable report to w.
func PrintReport(w io.Writer, report Report, projectFilter string) {
	// Header.
	if projectFilter != "" {
		name := filepath.Base(projectFilter)
		fmt.Fprintf(w, "\n  Squawk Report — %s (%d days)\n", name, report.ActiveDays)
	} else {
		fmt.Fprintf(w, "\n  Squawk Report (%d days)\n", report.ActiveDays)
	}
	fmt.Fprintf(w, "  %s\n\n", strings.Repeat("─", 35))

	// Overall section.
	if projectFilter == "" {
		fmt.Fprintf(w, "  Overall\n")
	}
	fmt.Fprintf(w, "    Sessions       %d\n", report.Sessions)
	fmt.Fprintf(w, "    Interventions  %d\n", report.Interventions)
	fmt.Fprintf(w, "    Actions Saved  ~%d\n", report.EstimatedActionsSaved)

	// By Project (only when not filtered to a single project).
	if projectFilter == "" && len(report.ByProject) > 0 {
		fmt.Fprintf(w, "\n  By Project\n")

		// Sort projects by interventions descending.
		type projEntry struct {
			path   string
			report ProjectReport
		}
		var projects []projEntry
		for p, pr := range report.ByProject {
			projects = append(projects, projEntry{path: p, report: pr})
		}
		sort.Slice(projects, func(i, j int) bool {
			return projects[i].report.Interventions > projects[j].report.Interventions
		})
		for _, pe := range projects {
			fmt.Fprintf(w, "    %-40s %3d interventions  ~%d saved\n",
				pe.path, pe.report.Interventions, pe.report.EstimatedActionsSaved)
		}
	}

	// By Action.
	if len(report.ByAction) > 0 {
		fmt.Fprintf(w, "\n  By Action\n")

		// Find max for bar scaling.
		maxCount := 0
		for _, c := range report.ByAction {
			if c > maxCount {
				maxCount = c
			}
		}

		// Sort actions by count descending.
		type actionEntry struct {
			name  string
			count int
		}
		var actions []actionEntry
		for name, count := range report.ByAction {
			actions = append(actions, actionEntry{name: name, count: count})
		}
		sort.Slice(actions, func(i, j int) bool {
			return actions[i].count > actions[j].count
		})

		barWidth := 20
		for _, a := range actions {
			filled := 0
			if maxCount > 0 {
				filled = a.count * barWidth / maxCount
			}
			bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
			pct := 0
			if report.Interventions > 0 {
				pct = a.count * 100 / report.Interventions
			}
			fmt.Fprintf(w, "    %-8s %3d  %s  %d%%\n", a.name, a.count, bar, pct)
		}
	}

	// Top Rules.
	if len(report.ByRule) > 0 {
		fmt.Fprintf(w, "\n  Top Rules\n")
		limit := len(report.ByRule)
		if limit > 10 {
			limit = 10
		}
		for i, rc := range report.ByRule[:limit] {
			fmt.Fprintf(w, "    %2d. %-35s %d\n", i+1, rc.Name, rc.Count)
		}
	}

	// File Hotspots.
	if len(report.FileHotspots) > 0 {
		fmt.Fprintf(w, "\n  File Hotspots\n")
		limit := len(report.FileHotspots)
		if limit > 10 {
			limit = 10
		}
		for _, fh := range report.FileHotspots[:limit] {
			fmt.Fprintf(w, "    %-50s %d\n", fh.Path, fh.Count)
		}
	}

	fmt.Fprintln(w)
}

// PrintJSON writes the report as JSON to w.
func PrintJSON(w io.Writer, report Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}
