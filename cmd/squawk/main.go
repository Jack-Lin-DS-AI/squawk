// Package main provides the CLI entrypoint for squawk, a Claude Code
// behavior supervision tool.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Jack-Lin-DS-AI/squawk/internal/action"
	"github.com/Jack-Lin-DS-AI/squawk/internal/config"
	"github.com/Jack-Lin-DS-AI/squawk/internal/daemon"
	"github.com/Jack-Lin-DS-AI/squawk/internal/monitor"
	"github.com/Jack-Lin-DS-AI/squawk/internal/rules"
	"github.com/Jack-Lin-DS-AI/squawk/internal/stats"
	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var version = "dev" // injected at build time via -ldflags

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

	root.PersistentFlags().StringP("config", "c", configPath, "path to config file")

	root.AddCommand(
		newInitCmd(),
		newWatchCmd(),
		newSetupCmd(),
		newStopCmd(),
		newTeardownCmd(),
		newRulesCmd(),
		newStatusCmd(),
		newLogCmd(),
		newStatsCmd(),
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
			hooks, err := config.GenerateHooksConfig(cfg.Server.Port, nil)
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
	var daemonMode bool

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Start the squawk monitoring server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfigFromCmd(cmd)
			if err != nil {
				return err
			}

			sdir := squawkDir(cfg)

			// Daemon mode: re-exec as background process.
			if daemonMode && !daemon.IsDaemonProcess() {
				pid, err := daemon.Daemonize(sdir, os.Args)
				if err != nil {
					return fmt.Errorf("failed to daemonize: %w", err)
				}
				fmt.Printf("Squawk daemon started (PID %d)\n", pid)
				fmt.Printf("Logs: %s\n", daemon.DaemonLogPath(sdir))
				return nil
			}

			// If daemon child, acquire PID file.
			var pidFile *daemon.PIDFile
			if daemon.IsDaemonProcess() {
				pidFile, err = daemon.Acquire(sdir)
				if err != nil {
					return fmt.Errorf("failed to acquire PID file: %w", err)
				}
				defer pidFile.Release()
			}

			// Load rules from the configured directory.
			loadedRules, err := rules.LoadRules(cfg.RulesDir)
			if err != nil {
				return fmt.Errorf("failed to load rules: %w", err)
			}
			engine := rules.NewEngine(loadedRules)

			// Create activity tracker with a sliding window.
			tracker := monitor.NewTracker(trackerWindow)

			// Create action logger.
			if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0o755); err != nil {
				return fmt.Errorf("failed to create log directory: %w", err)
			}
			actionLogger, err := action.NewActionLogger(cfg.LogFile)
			if err != nil {
				return fmt.Errorf("failed to create action logger: %w", err)
			}
			defer actionLogger.Close()

			// Create action executor wrapped with logging.
			executor := action.NewLoggingExecutor(
				action.NewExecutor(log.Default()),
				actionLogger,
			)

			// Create and configure the HTTP server.
			addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
			// Generate admin token for this daemon session.
			adminToken, err := monitor.GenerateAdminToken(sdir)
			if err != nil {
				log.Printf("Warning: failed to generate admin token: %v", err)
			}

			srv := monitor.NewServer(addr, cfg.RulesDir, tracker, engine, executor, adminToken)

			// Bind port before logging start to avoid spurious entries on failure.
			listener, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("failed to listen on %s: %w", addr, err)
			}

			actionLogger.LogDaemonStart()

			fmt.Printf("Starting squawk on %s...\n", addr)
			fmt.Printf("Loaded %d rule(s) from %s\n", len(loadedRules), cfg.RulesDir)

			// Set up graceful shutdown on SIGINT/SIGTERM.
			httpServer := &http.Server{
				Handler:      srv,
				ReadTimeout:  gracefulStopTimeout,
				WriteTimeout: gracefulStopTimeout,
				IdleTimeout:  30 * time.Second,
			}

			errCh := make(chan error, 1)
			go func() {
				if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
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

			// Give in-flight requests time to complete.
			ctx, cancel := context.WithTimeout(context.Background(), gracefulStopTimeout)
			defer cancel()

			if err := httpServer.Shutdown(ctx); err != nil {
				return fmt.Errorf("failed to shutdown server gracefully: %w", err)
			}

			fmt.Println("Server stopped.")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&daemonMode, "daemon", "d", false, "run as background daemon")
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
	rulesCmd.AddCommand(newRulesEnableCmd())
	rulesCmd.AddCommand(newRulesDisableCmd())
	rulesCmd.AddCommand(newRulesRemoveCmd())

	return rulesCmd
}

// newRulesListCmd creates the `squawk rules list` subcommand.
func newRulesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all loaded rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfigFromCmd(cmd)
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
}

// newRulesAddCmd creates the `squawk rules add` subcommand for interactive
// rule creation.
func newRulesAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [name]",
		Short: "Interactively create a new rule",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfigFromCmd(cmd)
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
				cfg, _, err := loadConfigFromCmd(cmd)
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
			tracker := monitor.NewTracker(trackerWindow)
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
	cmd.Flags().String("scenario", "", "test scenario to run")
	return cmd
}

// buildTestScenario returns a sequence of simulated events for the given
// scenario name.
func buildTestScenario(scenario string) ([]types.Event, string, error) {
	now := time.Now()
	const session = "test-session-001"

	event := func(hook, tool string, input map[string]any, ago time.Duration) types.Event {
		return types.Event{
			SessionID:     session,
			HookEventName: hook,
			ToolName:      tool,
			ToolInput:     input,
			Timestamp:     now.Add(-ago),
		}
	}
	file := func(path string) map[string]any { return map[string]any{"file_path": path} }
	cmd := func(c string) map[string]any { return map[string]any{"command": c} }

	switch scenario {
	case "test-modification":
		return []types.Event{
			event("PostToolUse", "Edit", file("pkg/handler/handler_test.go"), 4*time.Minute),
			event("PostToolUse", "Write", file("pkg/handler/handler_test.go"), 3*time.Minute),
			event("PostToolUse", "Edit", file("pkg/handler/handler_test.go"), 2*time.Minute),
			event("PreToolUse", "Edit", file("pkg/handler/handler_test.go"), 0),
		}, "Repeated test file modifications without reading source code", nil

	case "excessive-retry":
		return []types.Event{
			event("PostToolUseFailure", "Bash", cmd("go build ./..."), 2*time.Minute),
			event("PostToolUseFailure", "Bash", cmd("go build ./..."), 1*time.Minute),
			event("PostToolUseFailure", "Bash", cmd("go build ./..."), 0),
		}, "Same command failing repeatedly", nil

	case "blind-creation":
		return []types.Event{
			event("PostToolUse", "Write", file("pkg/auth/auth.go"), 4*time.Minute),
			event("PostToolUse", "Write", file("pkg/auth/middleware.go"), 3*time.Minute),
			event("PostToolUse", "Write", file("pkg/auth/token.go"), 2*time.Minute),
		}, "Creating multiple files without reading existing code", nil

	default:
		return nil, "", fmt.Errorf("unknown scenario %q (options: test-modification, excessive-retry, blind-creation)", scenario)
	}
}

// newSetupCmd creates the `squawk setup` one-command bootstrap.
func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "One-command setup: init + hooks + daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")

			// Create .squawk directory and config.
			if err := os.MkdirAll(".squawk", 0o755); err != nil {
				return fmt.Errorf("failed to create .squawk directory: %w", err)
			}

			cfg := config.Default()
			_, statErr := os.Stat(cfgPath)
			switch {
			case os.IsNotExist(statErr):
				if err := config.Save(cfg, cfgPath); err != nil {
					return fmt.Errorf("failed to save default config: %w", err)
				}
				fmt.Println("Created .squawk/config.yaml")
			case statErr != nil:
				return fmt.Errorf("failed to check config file: %w", statErr)
			default:
				var loadErr error
				cfg, loadErr = config.Load(cfgPath)
				if loadErr != nil {
					return fmt.Errorf("failed to load config: %w", loadErr)
				}
				fmt.Println("Using existing .squawk/config.yaml")
			}

			// Install hooks into settings.json.
			settingsPath, err := config.SettingsPath()
			if err != nil {
				return fmt.Errorf("failed to determine settings path: %w", err)
			}
			// Load rules for dynamic PreToolUse matcher computation.
			setupRules, _ := rules.LoadRules(cfg.RulesDir)
			if err := config.InstallHooks(settingsPath, cfg.Server.Port, setupRules); err != nil {
				return fmt.Errorf("failed to install hooks: %w", err)
			}
			fmt.Printf("Installed hooks into %s\n", settingsPath)

			// Start daemon if not already running.
			sdir := squawkDir(cfg)
			running, pid, err := daemon.IsRunning(sdir)
			if err != nil {
				return fmt.Errorf("failed to check daemon status: %w", err)
			}
			if running {
				fmt.Printf("Daemon already running (PID %d)\n", pid)
			} else {
				// Do not pass --daemon; the env sentinel _SQUAWK_DAEMON=1
				// tells the child to acquire the PID file without re-daemonizing.
				watchArgs := []string{os.Args[0], "watch", "--config", cfgPath}
				newPID, err := daemon.Daemonize(sdir, watchArgs)
				if err != nil {
					return fmt.Errorf("failed to start daemon: %w", err)
				}
				fmt.Printf("Started daemon (PID %d)\n", newPID)
			}

			fmt.Println("\nRestart Claude Code to activate hooks.")
			return nil
		},
	}
}

// newStopCmd creates the `squawk stop` subcommand.
func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the squawk daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfigFromCmd(cmd)
			if err != nil {
				return err
			}

			sdir := squawkDir(cfg)
			running, pid, err := daemon.IsRunning(sdir)
			if err != nil {
				return fmt.Errorf("failed to check daemon status: %w", err)
			}
			if !running {
				fmt.Println("No daemon running.")
				return nil
			}

			fmt.Printf("Stopping daemon (PID %d)...\n", pid)
			if err := daemon.StopDaemon(sdir, gracefulStopTimeout); err != nil {
				return fmt.Errorf("failed to stop daemon: %w", err)
			}
			fmt.Println("Daemon stopped.")
			return nil
		},
	}
}

// newTeardownCmd creates the `squawk teardown` subcommand.
func newTeardownCmd() *cobra.Command {
	var removeData bool

	cmd := &cobra.Command{
		Use:   "teardown",
		Short: "Stop daemon and remove hooks",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfigFromCmd(cmd)
			if err != nil {
				return err
			}

			// Stop daemon if running.
			sdir := squawkDir(cfg)
			running, pid, err := daemon.IsRunning(sdir)
			if err != nil {
				fmt.Printf("Warning: failed to check daemon status: %v\n", err)
			}
			if running {
				fmt.Printf("Stopping daemon (PID %d)...\n", pid)
				if err := daemon.StopDaemon(sdir, gracefulStopTimeout); err != nil {
					fmt.Printf("Warning: failed to stop daemon: %v\n", err)
				} else {
					fmt.Println("Daemon stopped.")
				}
			}

			// Remove hooks from settings.json.
			settingsPath, err := config.SettingsPath()
			if err != nil {
				return fmt.Errorf("failed to determine settings path: %w", err)
			}
			if err := config.UninstallHooks(settingsPath, cfg.Server.Port); err != nil {
				fmt.Printf("Warning: failed to remove hooks: %v\n", err)
			} else {
				fmt.Printf("Removed hooks from %s\n", settingsPath)
			}

			// Optionally remove data.
			if removeData {
				if err := os.RemoveAll(sdir); err != nil {
					return fmt.Errorf("failed to remove squawk directory %q: %w", sdir, err)
				}
				fmt.Printf("Removed %s directory.\n", sdir)
			}

			fmt.Println("\nRestart Claude Code to deactivate hooks.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&removeData, "remove-data", false, "also delete .squawk/ directory")
	return cmd
}

// newRulesEnableCmd creates the `squawk rules enable <name>` subcommand.
func newRulesEnableCmd() *cobra.Command {
	return newRulesToggleCmd("enable", rules.EnableRule)
}

// newRulesDisableCmd creates the `squawk rules disable <name>` subcommand.
func newRulesDisableCmd() *cobra.Command {
	return newRulesToggleCmd("disable", rules.DisableRule)
}

// newRulesToggleCmd creates an enable or disable subcommand.
func newRulesToggleCmd(verb string, fn func(string, string) (string, error)) *cobra.Command {
	// Capitalize first letter for display messages.
	titled := strings.ToUpper(verb[:1]) + verb[1:]
	return &cobra.Command{
		Use:   verb + " <name>",
		Short: titled + " a rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfigFromCmd(cmd)
			if err != nil {
				return err
			}

			path, err := fn(cfg.RulesDir, args[0])
			if err != nil {
				return fmt.Errorf("failed to %s rule: %w", verb, err)
			}
			fmt.Printf("%sd rule %q in %s\n", titled, args[0], path)

			triggerReload(cfg.Server.Port, squawkDir(cfg))
			return nil
		},
	}
}

// newRulesRemoveCmd creates the `squawk rules remove <name>` subcommand.
func newRulesRemoveCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a rule permanently",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				return fmt.Errorf("use --force to confirm permanent removal of rule %q", args[0])
			}

			cfg, _, err := loadConfigFromCmd(cmd)
			if err != nil {
				return err
			}

			path, remaining, err := rules.RemoveRule(cfg.RulesDir, args[0])
			if err != nil {
				return fmt.Errorf("failed to remove rule: %w", err)
			}

			if remaining == 0 {
				fmt.Printf("Removed rule %q (deleted empty file %s)\n", args[0], path)
			} else {
				fmt.Printf("Removed rule %q from %s (%d rules remaining)\n", args[0], path, remaining)
			}

			triggerReload(cfg.Server.Port, squawkDir(cfg))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "confirm permanent removal")
	return cmd
}

// triggerReload sends a reload request to the running server. Fails silently
// if the server is not running (fail-open). It reads the admin token from the
// squawk directory to authenticate with the admin endpoint.
func triggerReload(port int, sdir string) {
	url := adminURL(port, "/admin/reload-rules")
	client := &http.Client{Timeout: httpClientTimeout}

	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	if token := monitor.ReadAdminToken(sdir); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		// Server not running — that's fine, rules will be picked up on next start.
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Println("Rules reloaded in running server.")
	} else {
		fmt.Printf("Warning: server returned %d when reloading rules\n", resp.StatusCode)
	}
}

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
