package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Jack-Lin-DS-AI/squawk/internal/config"
	"github.com/Jack-Lin-DS-AI/squawk/internal/daemon"
	"github.com/Jack-Lin-DS-AI/squawk/internal/rules"
	"github.com/spf13/cobra"
)

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
