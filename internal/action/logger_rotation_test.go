package action

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
)

func TestLogRotation_TriggersWhenOverSize(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "squawk.log")

	logger, err := NewActionLogger(logFile)
	if err != nil {
		t.Fatalf("failed to create action logger: %v", err)
	}
	defer logger.Close()

	// Write enough data to exceed maxLogSize (10 MB).
	// Each entry is roughly 150 bytes of JSON. Write ~70k entries to exceed 10 MB.
	bigMessage := strings.Repeat("x", 1000)
	match := types.RuleMatch{
		Rule: types.Rule{
			Name:    "rotation-test",
			Enabled: true,
			Action:  types.Action{Type: types.ActionLog, Message: bigMessage},
		},
		MatchedAt: time.Now(),
	}

	// Write entries until the file exceeds maxLogSize.
	for i := 0; i < 12000; i++ {
		logger.LogAction(match, nil)
	}

	// The rotated file should exist.
	rotatedPath := logFile + ".1"
	info, err := os.Stat(rotatedPath)
	if err != nil {
		t.Fatalf("rotated file %q should exist: %v", rotatedPath, err)
	}
	if info.Size() == 0 {
		t.Fatal("rotated file should not be empty")
	}

	// The current log file should exist and be smaller than the rotated one
	// (it was freshly created after rotation).
	currentInfo, err := os.Stat(logFile)
	if err != nil {
		t.Fatalf("current log file should exist: %v", err)
	}
	if currentInfo.Size() >= info.Size() {
		t.Errorf("current file (%d bytes) should be smaller than rotated file (%d bytes)",
			currentInfo.Size(), info.Size())
	}

	// Verify the current log file contains valid JSON-lines.
	entries, err := ReadLogEntries(logFile, 0)
	if err != nil {
		t.Fatalf("failed to read current log entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("current log file should have entries after rotation")
	}
}

func TestLogRotation_NoRotateUnderSize(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "squawk.log")

	logger, err := NewActionLogger(logFile)
	if err != nil {
		t.Fatalf("failed to create action logger: %v", err)
	}
	defer logger.Close()

	// Write a few small entries that will not exceed 10 MB.
	for i := 0; i < 10; i++ {
		logger.LogAction(types.RuleMatch{
			Rule: types.Rule{
				Name:    "small-entry",
				Enabled: true,
				Action:  types.Action{Type: types.ActionLog, Message: "small"},
			},
			MatchedAt: time.Now(),
		}, nil)
	}

	// No rotated file should exist.
	rotatedPath := logFile + ".1"
	if _, err := os.Stat(rotatedPath); !os.IsNotExist(err) {
		t.Errorf("rotated file should not exist for small log, err = %v", err)
	}

	// All entries should be in the current file.
	entries, err := ReadLogEntries(logFile, 0)
	if err != nil {
		t.Fatalf("failed to read log entries: %v", err)
	}
	if len(entries) != 10 {
		t.Errorf("expected 10 entries, got %d", len(entries))
	}
}

func TestLogRotation_RotatedFileContainsValidJSON(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "squawk.log")

	logger, err := NewActionLogger(logFile)
	if err != nil {
		t.Fatalf("failed to create action logger: %v", err)
	}
	defer logger.Close()

	bigMessage := strings.Repeat("y", 1000)
	match := types.RuleMatch{
		Rule: types.Rule{
			Name:    "valid-json-test",
			Enabled: true,
			Action:  types.Action{Type: types.ActionLog, Message: bigMessage},
		},
		MatchedAt: time.Now(),
	}

	for i := 0; i < 12000; i++ {
		logger.LogAction(match, nil)
	}

	// Verify the rotated file contains valid JSON-lines.
	rotatedPath := logFile + ".1"
	entries, err := ReadLogEntries(rotatedPath, 0)
	if err != nil {
		t.Fatalf("failed to read rotated log entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("rotated file should contain entries")
	}
	for _, e := range entries {
		if e.RuleName != "valid-json-test" {
			t.Errorf("unexpected rule name in rotated file: %q", e.RuleName)
			break
		}
	}
}

func TestLogRotation_OverwritesPreviousRotation(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "squawk.log")

	// Create a pre-existing .1 file with known sentinel content.
	rotatedPath := logFile + ".1"
	sentinel := LogEntry{
		Timestamp: time.Now(),
		RuleName:  "old-sentinel",
		Action:    "log",
		Message:   "should be overwritten",
	}
	sentinelData, _ := json.Marshal(sentinel)
	sentinelData = append(sentinelData, '\n')
	if err := os.WriteFile(rotatedPath, sentinelData, 0o600); err != nil {
		t.Fatalf("failed to write sentinel file: %v", err)
	}

	logger, err := NewActionLogger(logFile)
	if err != nil {
		t.Fatalf("failed to create action logger: %v", err)
	}
	defer logger.Close()

	bigMessage := strings.Repeat("z", 1000)
	match := types.RuleMatch{
		Rule: types.Rule{
			Name:    "overwrite-test",
			Enabled: true,
			Action:  types.Action{Type: types.ActionLog, Message: bigMessage},
		},
		MatchedAt: time.Now(),
	}

	for i := 0; i < 12000; i++ {
		logger.LogAction(match, nil)
	}

	// The rotated file should now contain "overwrite-test" entries, not the sentinel.
	entries, err := ReadLogEntries(rotatedPath, 1)
	if err != nil {
		t.Fatalf("failed to read rotated entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected entries in rotated file")
	}
	if entries[0].RuleName == "old-sentinel" {
		t.Error("rotated file should have been overwritten, but sentinel still present")
	}
}

func TestFilePermissions_LogFile(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "squawk.log")

	logger, err := NewActionLogger(logFile)
	if err != nil {
		t.Fatalf("failed to create action logger: %v", err)
	}
	defer logger.Close()

	info, err := os.Stat(logFile)
	if err != nil {
		t.Fatalf("failed to stat log file: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("log file permissions = %o, want 0600", perm)
	}
}

func TestFilePermissions_PIDFile(t *testing.T) {
	// This test only verifies that Acquire creates the PID file with 0600.
	// It's duplicating daemon package logic but testing from action's perspective
	// is not possible, so we skip this here and rely on daemon_test.go.
	t.Skip("PID file permissions tested in daemon package")
}
