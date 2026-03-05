package main

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jack-Lin-DS-AI/squawk/internal/monitor"
	"github.com/Jack-Lin-DS-AI/squawk/internal/rules"
	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

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
