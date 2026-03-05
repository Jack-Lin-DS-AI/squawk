// Package main provides the CLI entrypoint for squawk, a Claude Code
// behavior supervision tool.
package main

import (
	"os"

	"github.com/spf13/cobra"
)

var version = "dev" // injected at build time via -ldflags

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
