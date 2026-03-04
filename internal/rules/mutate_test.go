package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
	"gopkg.in/yaml.v3"
)

func TestEnableRule(t *testing.T) {
	dir := setupMutateTestDir(t, []ruleFile{
		{Rules: []types.Rule{{Name: "test-rule", Enabled: false}}},
	})

	path, err := EnableRule(dir, "test-rule")
	if err != nil {
		t.Fatalf("EnableRule() error: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}

	rule := findTestRule(t, dir, "test-rule")
	if !rule.Enabled {
		t.Error("rule should be enabled after EnableRule()")
	}
}

func TestDisableRule(t *testing.T) {
	dir := setupMutateTestDir(t, []ruleFile{
		{Rules: []types.Rule{{Name: "test-rule", Enabled: true}}},
	})

	_, err := DisableRule(dir, "test-rule")
	if err != nil {
		t.Fatalf("DisableRule() error: %v", err)
	}

	rule := findTestRule(t, dir, "test-rule")
	if rule.Enabled {
		t.Error("rule should be disabled after DisableRule()")
	}
}

func TestEnableRule_NotFound(t *testing.T) {
	dir := setupMutateTestDir(t, []ruleFile{
		{Rules: []types.Rule{{Name: "other-rule", Enabled: true}}},
	})

	_, err := EnableRule(dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent rule")
	}
}

func TestRemoveRule_SingleRuleFile(t *testing.T) {
	dir := setupMutateTestDir(t, []ruleFile{
		{Rules: []types.Rule{{Name: "only-rule", Enabled: true}}},
	})

	path, remaining, err := RemoveRule(dir, "only-rule")
	if err != nil {
		t.Fatalf("RemoveRule() error: %v", err)
	}
	if remaining != 0 {
		t.Errorf("remaining = %d, want 0", remaining)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should be deleted when last rule removed")
	}
}

func TestRemoveRule_MultiRuleFile(t *testing.T) {
	dir := setupMutateTestDir(t, []ruleFile{
		{Rules: []types.Rule{
			{Name: "rule-a", Enabled: true},
			{Name: "rule-b", Enabled: true},
		}},
	})

	_, remaining, err := RemoveRule(dir, "rule-a")
	if err != nil {
		t.Fatalf("RemoveRule() error: %v", err)
	}
	if remaining != 1 {
		t.Errorf("remaining = %d, want 1", remaining)
	}

	if findTestRule(t, dir, "rule-b") == nil {
		t.Error("rule-b should still exist")
	}
	if findTestRule(t, dir, "rule-a") != nil {
		t.Error("rule-a should be removed")
	}
}

func TestRemoveRule_NotFound(t *testing.T) {
	dir := setupMutateTestDir(t, []ruleFile{
		{Rules: []types.Rule{{Name: "existing", Enabled: true}}},
	})

	_, _, err := RemoveRule(dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent rule")
	}
}

func TestEnableDisable_MultipleFiles(t *testing.T) {
	dir := setupMutateTestDir(t, []ruleFile{
		{Rules: []types.Rule{{Name: "rule-in-file-1", Enabled: true}}},
		{Rules: []types.Rule{{Name: "rule-in-file-2", Enabled: true}}},
	})

	_, err := DisableRule(dir, "rule-in-file-2")
	if err != nil {
		t.Fatalf("DisableRule() error: %v", err)
	}

	r1 := findTestRule(t, dir, "rule-in-file-1")
	if !r1.Enabled {
		t.Error("rule-in-file-1 should still be enabled")
	}

	r2 := findTestRule(t, dir, "rule-in-file-2")
	if r2.Enabled {
		t.Error("rule-in-file-2 should be disabled")
	}
}

func TestFindRule(t *testing.T) {
	dir := setupMutateTestDir(t, []ruleFile{
		{Rules: []types.Rule{
			{Name: "target-rule", Enabled: true},
			{Name: "other-rule", Enabled: false},
		}},
	})

	rule, path, err := FindRule(dir, "target-rule")
	if err != nil {
		t.Fatalf("FindRule() error: %v", err)
	}
	if rule == nil {
		t.Fatal("expected non-nil rule")
	}
	if rule.Name != "target-rule" {
		t.Errorf("name = %q, want target-rule", rule.Name)
	}
	if path == "" {
		t.Error("expected non-empty path")
	}
}

func TestFindRule_NotFound(t *testing.T) {
	dir := setupMutateTestDir(t, []ruleFile{
		{Rules: []types.Rule{{Name: "existing", Enabled: true}}},
	})

	_, _, err := FindRule(dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent rule")
	}
}

// setupMutateTestDir creates a temp directory with YAML rule files.
func setupMutateTestDir(t *testing.T, files []ruleFile) string {
	t.Helper()
	dir := t.TempDir()

	for i, rf := range files {
		data, err := yaml.Marshal(rf)
		if err != nil {
			t.Fatalf("failed to marshal test rules: %v", err)
		}
		name := filepath.Join(dir, fmt.Sprintf("rules-%02d.yaml", i))
		if err := os.WriteFile(name, data, 0o644); err != nil {
			t.Fatalf("failed to write test rule file: %v", err)
		}
	}

	return dir
}

// findTestRule loads all rules from dir and returns the named rule, or nil.
func findTestRule(t *testing.T, dir, name string) *types.Rule {
	t.Helper()
	rule, _, err := FindRule(dir, name)
	if err != nil {
		return nil
	}
	return rule
}
