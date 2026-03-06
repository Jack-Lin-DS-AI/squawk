package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestAcquire_NewPIDFile(t *testing.T) {
	dir := t.TempDir()

	pf, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}
	defer func() { _ = pf.Release() }()

	pid, err := ReadPID(dir)
	if err != nil {
		t.Fatalf("ReadPID() error: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("PID = %d, want %d", pid, os.Getpid())
	}
}

func TestAcquire_AlreadyLocked(t *testing.T) {
	dir := t.TempDir()

	pf1, err := Acquire(dir)
	if err != nil {
		t.Fatalf("first Acquire() error: %v", err)
	}
	defer func() { _ = pf1.Release() }()

	_, err = Acquire(dir)
	if err == nil {
		t.Fatal("second Acquire() should error when lock is held")
	}
}

func TestAcquire_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "subdir")

	pf, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}
	defer func() { _ = pf.Release() }()

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestRelease_CleansUp(t *testing.T) {
	dir := t.TempDir()

	pf, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	if err := pf.Release(); err != nil {
		t.Fatalf("Release() error: %v", err)
	}

	pidPath := filepath.Join(dir, pidFileName)
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("PID file still exists after Release")
	}

	// Acquire should succeed after release.
	pf2, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire() after Release() error: %v", err)
	}
	defer func() { _ = pf2.Release() }()
}

func TestRelease_Idempotent(t *testing.T) {
	dir := t.TempDir()

	pf, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	if err := pf.Release(); err != nil {
		t.Fatalf("first Release() error: %v", err)
	}
	if err := pf.Release(); err != nil {
		t.Fatalf("second Release() error: %v", err)
	}
}

func TestReadPID(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
		wantErr bool
	}{
		{"plain PID", "12345\n", 12345, false},
		{"no newline", "99999", 99999, false},
		{"whitespace", "  42  \n", 42, false},
		{"empty", "", 0, false},
		{"invalid", "not-a-number\n", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, pidFileName), []byte(tt.content), 0o644); err != nil {
				t.Fatalf("failed to write test PID file: %v", err)
			}

			pid, err := ReadPID(dir)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pid != tt.want {
				t.Errorf("ReadPID() = %d, want %d", pid, tt.want)
			}
		})
	}
}

func TestReadPID_NoFile(t *testing.T) {
	pid, err := ReadPID(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != 0 {
		t.Errorf("ReadPID() = %d, want 0", pid)
	}
}

func TestIsRunning_NoFile(t *testing.T) {
	running, pid, err := IsRunning(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if running {
		t.Error("IsRunning() = true, want false")
	}
	if pid != 0 {
		t.Errorf("pid = %d, want 0", pid)
	}
}

func TestIsRunning_LockedFile(t *testing.T) {
	dir := t.TempDir()

	pf, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}
	defer func() { _ = pf.Release() }()

	running, pid, err := IsRunning(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !running {
		t.Error("IsRunning() = false, want true")
	}
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", pid, os.Getpid())
	}
}

func TestIsRunning_UnlockedFile(t *testing.T) {
	dir := t.TempDir()

	// Stale PID file without lock.
	if err := os.WriteFile(filepath.Join(dir, pidFileName), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644); err != nil {
		t.Fatalf("failed to write test PID file: %v", err)
	}

	running, _, err := IsRunning(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if running {
		t.Error("IsRunning() = true, want false for stale PID file")
	}
}

func TestIsDaemonProcess(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"not set", "", false},
		{"set to 1", "1", true},
		{"set to true", "true", false},
		{"set to 0", "0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value == "" {
				os.Unsetenv(envSentinel)
			} else {
				t.Setenv(envSentinel, tt.value)
			}
			if got := IsDaemonProcess(); got != tt.want {
				t.Errorf("IsDaemonProcess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDaemonLogPath(t *testing.T) {
	tests := []struct {
		dir  string
		want string
	}{
		{"/tmp/.squawk", "/tmp/.squawk/daemon.log"},
		{"/home/user/.squawk", "/home/user/.squawk/daemon.log"},
	}

	for _, tt := range tests {
		if got := DaemonLogPath(tt.dir); got != tt.want {
			t.Errorf("DaemonLogPath(%q) = %q, want %q", tt.dir, got, tt.want)
		}
	}
}

func TestIsProcessGone(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"ESRCH", syscall.ESRCH, true},
		{"ErrProcessDone", os.ErrProcessDone, true},
		{"other", fmt.Errorf("some error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isProcessGone(tt.err); got != tt.want {
				t.Errorf("isProcessGone() = %v, want %v", got, tt.want)
			}
		})
	}
}
