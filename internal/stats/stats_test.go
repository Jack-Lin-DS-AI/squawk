package stats

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jack-Lin-DS-AI/squawk/internal/action"
)

func makeEntry(rule, act, session, project string, count int, ts time.Time) action.LogEntry {
	return action.LogEntry{
		Timestamp: ts, RuleName: rule, Action: act,
		SessionID: session, Project: project, ActivityCount: count,
	}
}

func TestCompute(t *testing.T) {
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)

	tests := []struct {
		name                  string
		entries               []action.LogEntry
		wantInterventions     int
		wantSessions          int
		wantActiveDays        int
		wantEstimatedSaved    int
		wantByAction          map[string]int
		wantTopRuleName       string
		wantProjectCount      int
	}{
		{
			name:               "empty log",
			entries:            nil,
			wantInterventions:  0,
			wantSessions:       0,
			wantActiveDays:     0,
			wantEstimatedSaved: 0,
			wantByAction:       map[string]int{},
			wantProjectCount:   0,
		},
		{
			name:               "single block",
			entries:            []action.LogEntry{makeEntry("test-rule", "block", "s1", "/projects/foo", 5, now)},
			wantInterventions:  1,
			wantSessions:       1,
			wantActiveDays:     1,
			wantEstimatedSaved: 8, // 5 + 3
			wantByAction:       map[string]int{"block": 1},
			wantTopRuleName:    "test-rule",
			wantProjectCount:   1,
		},
		{
			name: "mixed actions",
			entries: []action.LogEntry{
				makeEntry("rule-a", "block", "s1", "/projects/foo", 4, now),
				makeEntry("rule-b", "inject", "s1", "/projects/foo", 6, now),
				makeEntry("rule-c", "notify", "s1", "/projects/foo", 2, now),
			},
			wantInterventions:  3,
			wantSessions:       1,
			wantActiveDays:     1,
			wantEstimatedSaved: 11, // (4+3) + (6/2) + 1 = 7 + 3 + 1
			wantByAction:       map[string]int{"block": 1, "inject": 1, "notify": 1},
			wantTopRuleName:    "", // any of them, count is 1 each
			wantProjectCount:   1,
		},
		{
			name: "multiple sessions and projects",
			entries: []action.LogEntry{
				makeEntry("rule-a", "block", "s1", "/projects/foo", 2, now),
				makeEntry("rule-a", "block", "s2", "/projects/bar", 3, yesterday),
				makeEntry("rule-b", "inject", "s2", "/projects/bar", 4, yesterday),
			},
			wantInterventions:  3,
			wantSessions:       2,
			wantActiveDays:     2,
			wantEstimatedSaved: 13, // (2+3) + (3+3) + (4/2) = 5 + 6 + 2
			wantByAction:       map[string]int{"block": 2, "inject": 1},
			wantTopRuleName:    "rule-a",
			wantProjectCount:   2,
		},
		{
			name:               "old entries without session_id",
			entries:            []action.LogEntry{makeEntry("legacy-rule", "block", "", "", 0, now)},
			wantInterventions:  1,
			wantSessions:       0,
			wantActiveDays:     1,
			wantEstimatedSaved: 3, // 0 + 3 = 3 (min capped at 1, but 3 > 1)
			wantByAction:       map[string]int{"block": 1},
			wantTopRuleName:    "legacy-rule",
			wantProjectCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := Compute(tt.entries)

			if report.Interventions != tt.wantInterventions {
				t.Errorf("Interventions = %d, want %d", report.Interventions, tt.wantInterventions)
			}
			if report.Sessions != tt.wantSessions {
				t.Errorf("Sessions = %d, want %d", report.Sessions, tt.wantSessions)
			}
			if report.ActiveDays != tt.wantActiveDays {
				t.Errorf("ActiveDays = %d, want %d", report.ActiveDays, tt.wantActiveDays)
			}
			if report.EstimatedActionsSaved != tt.wantEstimatedSaved {
				t.Errorf("EstimatedActionsSaved = %d, want %d", report.EstimatedActionsSaved, tt.wantEstimatedSaved)
			}

			for act, want := range tt.wantByAction {
				if got := report.ByAction[act]; got != want {
					t.Errorf("ByAction[%q] = %d, want %d", act, got, want)
				}
			}

			if tt.wantTopRuleName != "" && len(report.ByRule) > 0 {
				if report.ByRule[0].Name != tt.wantTopRuleName {
					t.Errorf("top rule = %q, want %q", report.ByRule[0].Name, tt.wantTopRuleName)
				}
			}

			if len(report.ByProject) != tt.wantProjectCount {
				t.Errorf("project count = %d, want %d", len(report.ByProject), tt.wantProjectCount)
			}
		})
	}
}

func TestFilterSince(t *testing.T) {
	now := time.Now()
	entries := []action.LogEntry{
		{Timestamp: now.Add(-48 * time.Hour), RuleName: "old"},
		{Timestamp: now.Add(-12 * time.Hour), RuleName: "recent"},
		{Timestamp: now, RuleName: "now"},
	}

	filtered := FilterSince(entries, now.Add(-24*time.Hour))
	if len(filtered) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(filtered))
	}
	if filtered[0].RuleName != "recent" {
		t.Errorf("first = %q, want %q", filtered[0].RuleName, "recent")
	}
}

func TestFilterProject(t *testing.T) {
	entries := []action.LogEntry{
		{RuleName: "a", Project: "/projects/foo"},
		{RuleName: "b", Project: "/projects/bar"},
		{RuleName: "c", Project: "/projects/foo"},
	}

	filtered := FilterProject(entries, "/projects/foo")
	if len(filtered) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(filtered))
	}
}

func TestPrintReport(t *testing.T) {
	report := Report{
		ActiveDays:            7,
		Sessions:              10,
		Interventions:         25,
		EstimatedActionsSaved: 75,
		ByAction:              map[string]int{"block": 15, "inject": 10},
		ByRule: []RuleCount{
			{Name: "rule-a", Count: 15},
			{Name: "rule-b", Count: 10},
		},
		ByProject: map[string]ProjectReport{
			"/projects/foo": {Sessions: 6, Interventions: 15, EstimatedActionsSaved: 45},
			"/projects/bar": {Sessions: 4, Interventions: 10, EstimatedActionsSaved: 30},
		},
	}

	var buf bytes.Buffer
	PrintReport(&buf, report, "")
	output := buf.String()

	// Verify key sections appear.
	for _, want := range []string{
		"Squawk Report (7 days)",
		"Sessions       10",
		"Interventions  25",
		"Actions Saved  ~75",
		"By Project",
		"By Action",
		"block",
		"inject",
		"Top Rules",
		"rule-a",
		"rule-b",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestPrintReportWithProjectFilter(t *testing.T) {
	report := Report{
		ActiveDays:            3,
		Sessions:              5,
		Interventions:         12,
		EstimatedActionsSaved: 36,
		ByAction:              map[string]int{"block": 12},
		ByRule:                []RuleCount{{Name: "rule-a", Count: 12}},
		ByProject:             map[string]ProjectReport{},
	}

	var buf bytes.Buffer
	PrintReport(&buf, report, "/projects/foo")
	output := buf.String()

	if !strings.Contains(output, "Squawk Report — foo (3 days)") {
		t.Errorf("output missing project-filtered header, got:\n%s", output)
	}
	// Should not show "By Project" when filtering by project.
	if strings.Contains(output, "By Project") {
		t.Errorf("output should not contain 'By Project' when filtered")
	}
}

func TestPrintJSON(t *testing.T) {
	report := Report{
		ActiveDays:            1,
		Sessions:              2,
		Interventions:         3,
		EstimatedActionsSaved: 10,
		ByAction:              map[string]int{"block": 3},
		ByRule:                []RuleCount{{Name: "rule-a", Count: 3}},
		ByProject:             map[string]ProjectReport{},
	}

	var buf bytes.Buffer
	if err := PrintJSON(&buf, report); err != nil {
		t.Fatalf("PrintJSON error: %v", err)
	}

	var decoded Report
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("failed to decode JSON output: %v", err)
	}
	if decoded.Interventions != 3 {
		t.Errorf("Interventions = %d, want 3", decoded.Interventions)
	}
}

func TestEstimateSaved(t *testing.T) {
	tests := []struct {
		name          string
		action        string
		activityCount int
		want          int
	}{
		{"block with activities", "block", 5, 8},
		{"block with zero activities", "block", 0, 3},
		{"inject with activities", "inject", 6, 3},
		{"inject with zero activities", "inject", 0, 1},
		{"notify", "notify", 3, 1},
		{"log", "log", 2, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := action.LogEntry{Action: tt.action, ActivityCount: tt.activityCount}
			got := estimateSaved(e)
			if got != tt.want {
				t.Errorf("estimateSaved() = %d, want %d", got, tt.want)
			}
		})
	}
}
