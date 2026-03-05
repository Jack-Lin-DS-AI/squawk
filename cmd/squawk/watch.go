package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Jack-Lin-DS-AI/squawk/internal/action"
	"github.com/Jack-Lin-DS-AI/squawk/internal/daemon"
	"github.com/Jack-Lin-DS-AI/squawk/internal/monitor"
	"github.com/Jack-Lin-DS-AI/squawk/internal/rules"
	"github.com/spf13/cobra"
)

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
