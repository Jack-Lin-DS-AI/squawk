// Package daemon provides PID file management and process daemonization
// for running squawk as a background service.
package daemon

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// envSentinel is set in the child process to distinguish daemon children
	// from the parent process that initiated daemonization.
	envSentinel = "_SQUAWK_DAEMON"

	pidFileName   = "squawk.pid"
	daemonLogName = "daemon.log"
)

// PIDFile represents a locked PID file that indicates a running daemon.
type PIDFile struct {
	path string
	file *os.File
}

// Acquire creates or opens the PID file at <dir>/squawk.pid, acquires an
// exclusive flock (LOCK_EX|LOCK_NB), and writes the current PID.
// Returns an error if another process holds the lock.
func Acquire(dir string) (*PIDFile, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create PID directory: %w", err)
	}

	pidPath := filepath.Join(dir, pidFileName)
	f, err := os.OpenFile(pidPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to open PID file: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to acquire lock (another daemon may be running): %w", err)
	}

	if err := f.Truncate(0); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		return nil, fmt.Errorf("failed to truncate PID file: %w", err)
	}
	if _, err := fmt.Fprintf(f, "%d\n", os.Getpid()); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		return nil, fmt.Errorf("failed to write PID: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		return nil, fmt.Errorf("failed to sync PID file: %w", err)
	}

	return &PIDFile{path: pidPath, file: f}, nil
}

// Release releases the flock and removes the PID file.
func (p *PIDFile) Release() error {
	if p.file == nil {
		return nil
	}
	if err := syscall.Flock(int(p.file.Fd()), syscall.LOCK_UN); err != nil {
		p.file.Close()
		return fmt.Errorf("failed to release lock: %w", err)
	}
	if err := p.file.Close(); err != nil {
		return fmt.Errorf("failed to close PID file: %w", err)
	}
	p.file = nil
	if err := os.Remove(p.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to remove PID file: %w", err)
	}
	return nil
}

// ReadPID reads the PID number from the PID file in the given directory.
// Returns 0 and nil error if the file does not exist.
func ReadPID(dir string) (int, error) {
	data, err := os.ReadFile(filepath.Join(dir, pidFileName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read PID file: %w", err)
	}
	pidStr := strings.TrimSpace(string(data))
	if pidStr == "" {
		return 0, nil
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse PID %q: %w", pidStr, err)
	}
	return pid, nil
}

// IsRunning checks if a daemon is running by trying a non-blocking flock.
// If the lock fails, a daemon holds it. If it succeeds, no daemon is running.
func IsRunning(dir string) (bool, int, error) {
	pidPath := filepath.Join(dir, pidFileName)
	f, err := os.OpenFile(pidPath, os.O_RDWR, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, 0, nil
		}
		return false, 0, fmt.Errorf("failed to open PID file: %w", err)
	}
	defer f.Close()

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		// Lock failed — daemon is running.
		pid, readErr := ReadPID(dir)
		if readErr != nil {
			return true, 0, readErr
		}
		return true, pid, nil
	}
	// Lock succeeded — no daemon. Release immediately.
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false, 0, nil
}

// IsDaemonProcess returns true if this process was spawned as a daemon child.
func IsDaemonProcess() bool {
	return os.Getenv(envSentinel) == "1"
}

// Daemonize re-execs the current binary as a background daemon. It sets
// _SQUAWK_DAEMON=1, detaches via Setsid, and redirects output to daemon.log.
// Returns the child PID.
func Daemonize(squawkDir string, args []string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("failed to get executable path: %w", err)
	}

	if err := os.MkdirAll(squawkDir, 0o755); err != nil {
		return 0, fmt.Errorf("failed to create squawk directory: %w", err)
	}

	logPath := DaemonLogPath(squawkDir)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, fmt.Errorf("failed to open daemon log file: %w", err)
	}

	var childArgs []string
	if len(args) > 1 {
		childArgs = args[1:]
	}

	cmd := exec.Command(exe, childArgs...)
	cmd.Env = append(os.Environ(), envSentinel+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return 0, fmt.Errorf("failed to start daemon process: %w", err)
	}

	pid := cmd.Process.Pid
	logFile.Close()
	_ = cmd.Process.Release()
	return pid, nil
}

// StopDaemon sends SIGTERM to the daemon, waits up to timeout, then SIGKILL.
func StopDaemon(dir string, timeout time.Duration) error {
	pid, err := ReadPID(dir)
	if err != nil {
		return fmt.Errorf("failed to read daemon PID: %w", err)
	}
	if pid == 0 {
		return fmt.Errorf("no PID file found; daemon may not be running")
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process %d: %w", pid, err)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		if isProcessGone(err) {
			removePIDFile(dir)
			return nil
		}
		return fmt.Errorf("failed to send SIGTERM to process %d: %w", pid, err)
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := process.Signal(syscall.Signal(0)); err != nil {
			removePIDFile(dir)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := process.Signal(syscall.SIGKILL); err != nil {
		if isProcessGone(err) {
			removePIDFile(dir)
			return nil
		}
		return fmt.Errorf("failed to send SIGKILL to process %d: %w", pid, err)
	}

	removePIDFile(dir)
	return nil
}

// DaemonLogPath returns the path to the daemon log file.
func DaemonLogPath(dir string) string {
	return filepath.Join(dir, daemonLogName)
}

func isProcessGone(err error) bool {
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH)
}

func removePIDFile(dir string) {
	os.Remove(filepath.Join(dir, pidFileName))
}
