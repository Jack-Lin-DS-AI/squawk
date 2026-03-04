package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAcquire_PIDFilePermissions(t *testing.T) {
	dir := t.TempDir()

	pf, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}
	defer pf.Release()

	pidPath := filepath.Join(dir, pidFileName)
	info, err := os.Stat(pidPath)
	if err != nil {
		t.Fatalf("failed to stat PID file: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("PID file permissions = %o, want 0600", perm)
	}
}
