package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Jack-Lin-DS-AI/squawk/internal/action"
	"github.com/Jack-Lin-DS-AI/squawk/internal/config"
	"github.com/Jack-Lin-DS-AI/squawk/internal/daemon"
	"github.com/Jack-Lin-DS-AI/squawk/internal/rules"
	"github.com/Jack-Lin-DS-AI/squawk/internal/stats"
	"github.com/spf13/cobra"
)

// newStatusCmd creates the `squawk status` subcommand that shows daemon,
// hooks, and session status.
func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon, hooks, and session status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfigFromCmd(cmd)
			if err != nil {
				return err
			}

			sdir := squawkDir(cfg)

			// Daemon status.
			running, pid, err := daemon.IsRunning(sdir)
			if err != nil {
				fmt.Printf("Daemon:  unknown (%v)\n", err)
			} else if running {
				fmt.Printf("Daemon:  running (PID %d)\n", pid)
			} else {
				fmt.Println("Daemon:  not running")
			}

			// Hooks status.
			settingsPath, err := config.SettingsPath()
			if err == nil {
				installed, _ := config.IsHooksInstalled(settingsPath, cfg.Server.Port)
				if installed {
					fmt.Println("Hooks:   installed")
				} else {
					fmt.Println("Hooks:   not installed")
				}
			}

			// Server status (sessions).
			if running {
				url := adminURL(cfg.Server.Port, "/status")
				client := &http.Client{Timeout: httpClientTimeout}
				resp, err := client.Get(url)
				if err == nil {
					defer resp.Body.Close()
					body, _ := io.ReadAll(resp.Body)
					var status struct {
						Sessions map[string]int `json:"sessions"`
					}
					if json.Unmarshal(body, &status) == nil {
						fmt.Printf("Sessions:%d active\n", len(status.Sessions))
					}
				}
			}

			// Rules status.
			loadedRules, err := rules.LoadRules(cfg.RulesDir)
			if err != nil {
				fmt.Printf("Rules:   error (%v)\n", err)
			} else if len(loadedRules) == 0 {
				fmt.Println("Rules:   none loaded")
			} else {
				var enabled int
				for _, r := range loadedRules {
					if r.Enabled {
						enabled++
					}
				}
				fmt.Printf("Rules:   %d enabled / %d total\n", enabled, len(loadedRules))
			}

			return nil
		},
	}
}

// newLogCmd creates the `squawk log` subcommand that reads and displays
// recent entries from the action log file.
func newLogCmd() *cobra.Command {
	var tail int

	cmd := &cobra.Command{
		Use:   "log",
		Short: "Display recent action log entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfigFromCmd(cmd)
			if err != nil {
				return err
			}

			entries, err := action.ReadLogEntries(cfg.LogFile, tail)
			if err != nil {
				return err
			}

			if len(entries) == 0 {
				fmt.Println("No log entries found.")
				return nil
			}

			fmt.Printf("%-25s %-30s %-10s %s\n", "TIMESTAMP", "RULE", "ACTION", "MESSAGE")
			fmt.Printf("%-25s %-30s %-10s %s\n", "---------", "----", "------", "-------")
			for _, entry := range entries {
				ts := entry.Timestamp.Format("2006-01-02 15:04:05")
				// Truncate long messages for display.
				msg := strings.TrimSpace(entry.Message)
				if len(msg) > 80 {
					msg = msg[:77] + "..."
				}
				fmt.Printf("%-25s %-30s %-10s %s\n", ts, entry.RuleName, entry.Action, msg)
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&tail, "tail", 10, "number of recent entries to display")
	return cmd
}

// newStatsCmd creates the `squawk stats` subcommand that computes and
// displays aggregated metrics from the action log.
func newStatsCmd() *cobra.Command {
	var (
		since   string
		asJSON  bool
		project string
	)

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Display intervention statistics and metrics",
		Long: `Compute and display aggregated metrics from squawk's action log,
including intervention counts, estimated actions saved, per-project
breakdowns, and top rules.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfigFromCmd(cmd)
			if err != nil {
				return err
			}

			entries, err := action.ReadLogEntries(cfg.LogFile, 0)
			if err != nil {
				return err
			}

			// Apply --since filter.
			if since != "" {
				d, err := parseDuration(since)
				if err != nil {
					return fmt.Errorf("invalid --since value %q: %w", since, err)
				}
				entries = stats.FilterSince(entries, time.Now().Add(-d))
			}

			// Apply --project filter.
			if project != "" {
				entries = stats.FilterProject(entries, project)
			}

			report := stats.Compute(entries)

			if asJSON {
				return stats.PrintJSON(os.Stdout, report)
			}

			stats.PrintReport(os.Stdout, report, project)
			return nil
		},
	}

	cmd.Flags().StringVar(&since, "since", "", "time window filter (e.g. 7d, 30d, 24h)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	cmd.Flags().StringVar(&project, "project", "", "filter to a specific project path")
	return cmd
}
