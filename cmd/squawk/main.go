// Package main provides the CLI entrypoint for squawk, a Claude Code
// behavior supervision tool.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Jack-Lin-DS-AI/squawk/internal/action"
	"github.com/Jack-Lin-DS-AI/squawk/internal/config"
	"github.com/Jack-Lin-DS-AI/squawk/internal/monitor"
	"github.com/Jack-Lin-DS-AI/squawk/internal/rules"
	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	version    = "0.1.0"
	configPath = ".squawk/config.yaml"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "squawk",
		Short:   "Claude Code behavior supervision tool",
		Long:    "Squawk monitors Claude Code tool usage via hooks and enforces supervision rules in real time.",
		Version: version,
	}

	root.AddCommand(
		newInitCmd(),
		newWatchCmd(),
		newRulesCmd(),
		newStatusCmd(),
		newLogCmd(),
	)

	return root
}

// newInitCmd creates the `squawk init` subcommand that bootstraps a project
// with default config and prints the hooks snippet for settings.json.
func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize squawk in the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Create .squawk directory.
			if err := os.MkdirAll(".squawk", 0o755); err != nil {
				return fmt.Errorf("failed to create .squawk directory: %w", err)
			}

			// Write default config.
			cfg := config.Default()
			if err := config.Save(cfg, configPath); err != nil {
				return fmt.Errorf("failed to save default config: %w", err)
			}
			fmt.Println("Created .squawk/config.yaml with default settings.")

			// Generate and print hooks config.
			hooks, err := config.GenerateHooksConfig(cfg.Server.Port)
			if err != nil {
				return fmt.Errorf("failed to generate hooks config: %w", err)
			}

			hooksJSON, err := json.MarshalIndent(hooks, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal hooks config: %w", err)
			}

			fmt.Println()
			fmt.Println("Add the following to your Claude Code settings.json (~/.claude/settings.json):")
			fmt.Println()
			fmt.Println(string(hooksJSON))
			fmt.Println()
			fmt.Println("Then run: squawk watch")

			return nil
		},
	}
}

// newWatchCmd creates the `squawk watch` subcommand that starts the HTTP
// monitoring server with full rule engine integration and graceful shutdown.
func newWatchCmd() *cobra.Command {
	var cfgPath string

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Start the squawk monitoring server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(cfgPath)
			if err != nil {
				return err
			}

			// Load rules from the configured directory.
			loadedRules, err := rules.LoadRules(cfg.RulesDir)
			if err != nil {
				return fmt.Errorf("failed to load rules: %w", err)
			}
			engine := rules.NewEngine(loadedRules)

			// Create activity tracker with a 10-minute sliding window.
			tracker := monitor.NewTracker(10 * time.Minute)

			// Create action logger.
			if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0o755); err != nil {
				return fmt.Errorf("failed to create log directory: %w", err)
			}
			actionLogger, err := action.NewActionLogger(cfg.LogFile)
			if err != nil {
				return fmt.Errorf("failed to create action logger: %w", err)
			}
			defer actionLogger.Close()

			// Create action executor.
			executor := action.NewExecutor(log.Default())

			// Create and configure the HTTP server.
			addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
			srv := monitor.NewServer(addr, tracker, engine, executor)

			fmt.Printf("Starting squawk on %s...\n", addr)
			fmt.Printf("Loaded %d rule(s) from %s\n", len(loadedRules), cfg.RulesDir)

			// Set up graceful shutdown on SIGINT/SIGTERM.
			httpServer := &http.Server{
				Addr:         addr,
				Handler:      srv,
				ReadTimeout:  5 * time.Second,
				WriteTimeout: 5 * time.Second,
				IdleTimeout:  30 * time.Second,
			}

			errCh := make(chan error, 1)
			go func() {
				if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					errCh <- fmt.Errorf("server error: %w", err)
				}
				close(errCh)
			}()

			// Wait for shutdown signal.
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

			select {
			case sig := <-sigCh:
				fmt.Printf("\nReceived %s, shutting down...\n", sig)
			case err := <-errCh:
				if err != nil {
					return err
				}
			}

			// Give in-flight requests 5 seconds to complete.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := httpServer.Shutdown(ctx); err != nil {
				return fmt.Errorf("failed to shutdown server gracefully: %w", err)
			}

			fmt.Println("Server stopped.")
			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", configPath, "path to config file")
	return cmd
}

// newRulesCmd creates the `squawk rules` subcommand group.
func newRulesCmd() *cobra.Command {
	rulesCmd := &cobra.Command{
		Use:   "rules",
		Short: "Manage supervision rules",
	}

	rulesCmd.AddCommand(newRulesListCmd())
	rulesCmd.AddCommand(newRulesTestCmd())
	rulesCmd.AddCommand(newRulesAddCmd())

	return rulesCmd
}

// newRulesListCmd creates the `squawk rules list` subcommand.
func newRulesListCmd() *cobra.Command {
	var cfgPath string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all loaded rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(cfgPath)
			if err != nil {
				return err
			}

			loadedRules, err := rules.LoadRules(cfg.RulesDir)
			if err != nil {
				return fmt.Errorf("failed to load rules: %w", err)
			}

			if len(loadedRules) == 0 {
				fmt.Println("No rules found.")
				return nil
			}

			fmt.Printf("%-30s %-8s %s\n", "NAME", "ENABLED", "DESCRIPTION")
			fmt.Printf("%-30s %-8s %s\n", "----", "-------", "-----------")
			for _, r := range loadedRules {
				enabled := "no"
				if r.Enabled {
					enabled = "yes"
				}
				fmt.Printf("%-30s %-8s %s\n", r.Name, enabled, r.Description)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", configPath, "path to config file")
	return cmd
}

// newRulesAddCmd creates the `squawk rules add` subcommand for interactive
// rule creation.
func newRulesAddCmd() *cobra.Command {
	var cfgPath string

	cmd := &cobra.Command{
		Use:   "add [name]",
		Short: "Interactively create a new rule",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(cfgPath)
			if err != nil {
				return err
			}

			scanner := bufio.NewScanner(os.Stdin)

			// Rule name.
			var name string
			if len(args) > 0 {
				name = args[0]
			} else {
				name = promptInput(scanner, "Rule name")
			}
			if name == "" {
				return fmt.Errorf("rule name is required")
			}

			// Description.
			description := promptInput(scanner, "Description")

			// Trigger event type.
			fmt.Println("Event types: PreToolUse, PostToolUse, PostToolUseFailure")
			eventType := promptInput(scanner, "Trigger event type")
			if eventType == "" {
				eventType = "PostToolUse"
			}

			// Tool pattern.
			toolPattern := promptInput(scanner, "Tool pattern (regex, e.g. Edit|Write)")

			// File pattern.
			filePattern := promptInput(scanner, "File pattern (glob, optional, e.g. *.go)")

			// Count threshold.
			countStr := promptInput(scanner, "Count threshold (default: 1)")
			count := 1
			if countStr != "" {
				if _, err := fmt.Sscanf(countStr, "%d", &count); err != nil {
					return fmt.Errorf("failed to parse count: %w", err)
				}
			}

			// Time window.
			within := promptInput(scanner, "Time window (e.g. 5m, 10m, default: 5m)")
			if within == "" {
				within = "5m"
			}

			// Action type.
			fmt.Println("Action types: block, inject, notify, log")
			actionTypeStr := promptInput(scanner, "Action type")
			if actionTypeStr == "" {
				actionTypeStr = "log"
			}

			// Action message.
			actionMessage := promptInput(scanner, "Action message")

			// Build the rule.
			rule := types.Rule{
				Name:        name,
				Description: description,
				Enabled:     true,
				Priority:    5,
				Trigger: types.Trigger{
					Logic: "and",
					Conditions: []types.Condition{
						{
							Event:       eventType,
							Tool:        toolPattern,
							FilePattern: filePattern,
							Count:       count,
							Within:      within,
						},
					},
				},
				Action: types.Action{
					Type:    types.ActionType(actionTypeStr),
					Message: actionMessage,
				},
			}

			// Write the rule to a YAML file.
			ruleFile := struct {
				Rules []types.Rule `yaml:"rules"`
			}{
				Rules: []types.Rule{rule},
			}

			data, err := yaml.Marshal(ruleFile)
			if err != nil {
				return fmt.Errorf("failed to marshal rule: %w", err)
			}

			// Sanitize the rule name for use as a filename.
			filename := strings.ReplaceAll(strings.ToLower(name), " ", "-") + ".yaml"
			outPath := filepath.Join(cfg.RulesDir, filename)

			if err := os.MkdirAll(cfg.RulesDir, 0o755); err != nil {
				return fmt.Errorf("failed to create rules directory: %w", err)
			}

			if err := os.WriteFile(outPath, data, 0o644); err != nil {
				return fmt.Errorf("failed to write rule file: %w", err)
			}

			fmt.Printf("Rule %q written to %s\n", name, outPath)
			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", configPath, "path to config file")
	return cmd
}

// promptInput prints a prompt and reads a line of input from the scanner.
func promptInput(scanner *bufio.Scanner, prompt string) string {
	fmt.Printf("%s: ", prompt)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

// newRulesTestCmd creates the `squawk rules test` subcommand that simulates
// events against loaded rules using built-in test scenarios.
func newRulesTestCmd() *cobra.Command {
	var rulesDir string
	var cfgPath string

	cmd := &cobra.Command{
		Use:   "test --scenario <name>",
		Short: "Test rules against simulated events",
		Long: `Simulate events against loaded rules using built-in test scenarios.

Available scenarios:
  test-modification     Simulate repeated test file edits without reading source
  excessive-retry       Simulate a command failing repeatedly
  blind-creation        Simulate creating files without reading existing code`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Determine rules directory.
			dir := rulesDir
			if dir == "" {
				cfg, err := loadConfig(cfgPath)
				if err != nil {
					return err
				}
				dir = cfg.RulesDir
			}

			loadedRules, err := rules.LoadRules(dir)
			if err != nil {
				return fmt.Errorf("failed to load rules: %w", err)
			}
			engine := rules.NewEngine(loadedRules)

			scenario, _ := cmd.Flags().GetString("scenario")
			if scenario == "" {
				return fmt.Errorf("--scenario flag is required (options: test-modification, excessive-retry, blind-creation)")
			}

			events, description, err := buildTestScenario(scenario)
			if err != nil {
				return err
			}

			fmt.Printf("Scenario: %s\n", description)
			fmt.Printf("Rules loaded: %d\n", len(loadedRules))
			fmt.Println(strings.Repeat("-", 60))

			// Run events through the engine, accumulating activities.
			tracker := monitor.NewTracker(10 * time.Minute)
			var totalMatches int

			for i, event := range events {
				// Record the event to build up activity history.
				tracker.Record(event)
				activities := tracker.GetActivities(event.SessionID)

				matches := engine.Evaluate(activities, event)

				fmt.Printf("\nEvent %d: %s tool=%s", i+1, event.HookEventName, event.ToolName)
				if filePath, ok := event.ToolInput["file_path"].(string); ok {
					fmt.Printf(" file=%s", filePath)
				}
				fmt.Println()

				if len(matches) == 0 {
					fmt.Println("  No rules matched.")
				} else {
					for _, m := range matches {
						totalMatches++
						fmt.Printf("  MATCH: rule=%q action=%s\n", m.Rule.Name, m.Rule.Action.Type)
						fmt.Printf("         message: %s\n", strings.TrimSpace(m.Rule.Action.Message))
					}
				}
			}

			fmt.Println(strings.Repeat("-", 60))
			fmt.Printf("Total events: %d, Total matches: %d\n", len(events), totalMatches)
			return nil
		},
	}

	cmd.Flags().StringVar(&rulesDir, "rules-dir", "", "path to rules directory (overrides config)")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", configPath, "path to config file")
	cmd.Flags().String("scenario", "", "test scenario to run")
	return cmd
}

// buildTestScenario returns a sequence of simulated events for the given
// scenario name.
func buildTestScenario(scenario string) ([]types.Event, string, error) {
	now := time.Now()
	sessionID := "test-session-001"

	switch scenario {
	case "test-modification":
		// Simulate editing test files 3 times without reading source code.
		events := []types.Event{
			{
				SessionID:     sessionID,
				HookEventName: "PostToolUse",
				ToolName:      "Edit",
				ToolInput:     map[string]any{"file_path": "pkg/handler/handler_test.go"},
				Timestamp:     now.Add(-4 * time.Minute),
			},
			{
				SessionID:     sessionID,
				HookEventName: "PostToolUse",
				ToolName:      "Write",
				ToolInput:     map[string]any{"file_path": "pkg/handler/handler_test.go"},
				Timestamp:     now.Add(-3 * time.Minute),
			},
			{
				SessionID:     sessionID,
				HookEventName: "PostToolUse",
				ToolName:      "Edit",
				ToolInput:     map[string]any{"file_path": "pkg/handler/handler_test.go"},
				Timestamp:     now.Add(-2 * time.Minute),
			},
			{
				SessionID:     sessionID,
				HookEventName: "PreToolUse",
				ToolName:      "Edit",
				ToolInput:     map[string]any{"file_path": "pkg/handler/handler_test.go"},
				Timestamp:     now,
			},
		}
		return events, "Repeated test file modifications without reading source code", nil

	case "excessive-retry":
		// Simulate a Bash command failing 3 times.
		events := []types.Event{
			{
				SessionID:     sessionID,
				HookEventName: "PostToolUseFailure",
				ToolName:      "Bash",
				ToolInput:     map[string]any{"command": "go build ./..."},
				Timestamp:     now.Add(-2 * time.Minute),
			},
			{
				SessionID:     sessionID,
				HookEventName: "PostToolUseFailure",
				ToolName:      "Bash",
				ToolInput:     map[string]any{"command": "go build ./..."},
				Timestamp:     now.Add(-1 * time.Minute),
			},
			{
				SessionID:     sessionID,
				HookEventName: "PostToolUseFailure",
				ToolName:      "Bash",
				ToolInput:     map[string]any{"command": "go build ./..."},
				Timestamp:     now,
			},
		}
		return events, "Same command failing repeatedly", nil

	case "blind-creation":
		// Simulate creating files without reading existing code.
		events := []types.Event{
			{
				SessionID:     sessionID,
				HookEventName: "PostToolUse",
				ToolName:      "Write",
				ToolInput:     map[string]any{"file_path": "pkg/auth/auth.go"},
				Timestamp:     now.Add(-4 * time.Minute),
			},
			{
				SessionID:     sessionID,
				HookEventName: "PostToolUse",
				ToolName:      "Write",
				ToolInput:     map[string]any{"file_path": "pkg/auth/middleware.go"},
				Timestamp:     now.Add(-3 * time.Minute),
			},
			{
				SessionID:     sessionID,
				HookEventName: "PostToolUse",
				ToolName:      "Write",
				ToolInput:     map[string]any{"file_path": "pkg/auth/token.go"},
				Timestamp:     now.Add(-2 * time.Minute),
			},
		}
		return events, "Creating multiple files without reading existing code", nil

	default:
		return nil, "", fmt.Errorf("unknown scenario %q (options: test-modification, excessive-retry, blind-creation)", scenario)
	}
}

// newStatusCmd creates the `squawk status` subcommand that queries the
// running squawk server's /status endpoint.
func newStatusCmd() *cobra.Command {
	var port int

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check the status of the running squawk server",
		RunE: func(cmd *cobra.Command, args []string) error {
			url := fmt.Sprintf("http://localhost:%d/status", port)

			resp, err := http.Get(url)
			if err != nil {
				return fmt.Errorf("failed to connect to squawk server at %s: %w", url, err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Errorf("failed to read response: %w", err)
			}

			fmt.Println(string(body))
			return nil
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 3131, "squawk server port")
	return cmd
}

// newLogCmd creates the `squawk log` subcommand that reads and displays
// recent entries from the action log file.
func newLogCmd() *cobra.Command {
	var tail int
	var cfgPath string

	cmd := &cobra.Command{
		Use:   "log",
		Short: "Display recent action log entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(cfgPath)
			if err != nil {
				return err
			}

			entries, err := readLogEntries(cfg.LogFile, tail)
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
	cmd.Flags().StringVarP(&cfgPath, "config", "c", configPath, "path to config file")
	return cmd
}

// readLogEntries reads the last n entries from the JSON-lines log file.
func readLogEntries(logFile string, n int) ([]action.LogEntry, error) {
	f, err := os.Open(logFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to open log file %q: %w", logFile, err)
	}
	defer f.Close()

	var all []action.LogEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry action.LogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // skip malformed lines
		}
		all = append(all, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read log file: %w", err)
	}

	if len(all) <= n {
		return all, nil
	}
	return all[len(all)-n:], nil
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
