// Package cli defines the tsk command-line interface using Cobra.
package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// NewRootCmd returns the root tsk Cobra command with all sub-commands attached.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "tsk",
		Short: "Local MCP server for safe AI agent tool calls",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			levelStr, _ := cmd.Flags().GetString("log-level")
			return initLogger(levelStr)
		},
	}

	root.PersistentFlags().String("log-level", "info", "Log level (debug, info, warn, error)")

	root.AddCommand(
		newInitCmd(),
		newRunCmd(),
		newLogsCmd(),
	)

	return root
}

func initLogger(levelStr string) error {
	var level slog.Level
	if err := level.UnmarshalText([]byte(levelStr)); err != nil {
		return fmt.Errorf("invalid log level %q: must be debug, info, warn, or error", levelStr)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	return nil
}
