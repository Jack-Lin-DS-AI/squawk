package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jack-Lin-DS-AI/squawk/internal/config"
	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
	"github.com/spf13/cobra"
)

const (
	configPath = ".squawk/config.yaml"

	httpClientTimeout   = 2 * time.Second
	gracefulStopTimeout = 5 * time.Second
	trackerWindow       = 10 * time.Minute
)

// squawkDir returns the .squawk directory derived from the config's log file path.
func squawkDir(cfg *types.Config) string {
	return filepath.Dir(cfg.LogFile)
}

// adminURL constructs a URL for the squawk admin API.
func adminURL(port int, path string) string {
	return fmt.Sprintf("http://localhost:%d%s", port, path)
}

// loadConfigFromCmd reads the --config flag from the command and loads the config.
func loadConfigFromCmd(cmd *cobra.Command) (*types.Config, string, error) {
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := loadConfig(cfgPath)
	return cfg, cfgPath, err
}

// loadConfig loads the config from the given path, falling back to defaults
// if the file does not exist.
func loadConfig(path string) (*types.Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return config.Default(), nil
	}

	cfg, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	return cfg, nil
}

// parseDuration parses a duration string that supports days (e.g. "7d", "30d")
// in addition to Go's standard duration format.
func parseDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		var days int
		if _, err := fmt.Sscanf(s, "%dd", &days); err != nil {
			return 0, fmt.Errorf("failed to parse days: %w", err)
		}
		if days <= 0 {
			return 0, fmt.Errorf("days must be positive, got %d", days)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}
