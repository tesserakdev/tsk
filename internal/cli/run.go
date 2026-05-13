package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/tesserakdev/tsk/internal/activitylog"
	"github.com/tesserakdev/tsk/internal/config"
	"github.com/tesserakdev/tsk/internal/mcp"
	"github.com/tesserakdev/tsk/internal/proxy"
	"github.com/tesserakdev/tsk/internal/ratelimit"
	"github.com/tesserakdev/tsk/internal/scrubber"
	"github.com/tesserakdev/tsk/internal/secrets"
	"github.com/tesserakdev/tsk/internal/store"
)

// resolveTskDir expands a leading ~ to the home directory.
func resolveTskDir(dir string) (string, error) {
	if strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home directory: %w", err)
		}
		return filepath.Join(home, dir[2:]), nil
	}
	if dir == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home directory: %w", err)
		}
		return home, nil
	}
	return dir, nil
}

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Start the MCP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _ := cmd.Flags().GetString("dir")
			tskDir, err := resolveTskDir(dir)
			if err != nil {
				return err
			}
			return runServer(tskDir)
		},
	}
	cmd.Flags().String("dir", "~/.tsk", "tsk directory (overrides default ~/.tsk)")
	return cmd
}

// buildServer performs all startup validation and construction, returning a
// configured mcp.Server ready to Serve. The caller is responsible for closing
// the returned *sql.DB when done.
func buildServer(ctx context.Context, tskDir string) (*mcp.Server, func(), error) {
	cfgProvider := config.NewFileProvider(filepath.Join(tskDir, "rules.yaml"))
	cfg, err := cfgProvider.Load(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}

	secProvider, err := secrets.NewFileProvider(filepath.Join(tskDir, ".secrets"))
	if err != nil {
		return nil, nil, fmt.Errorf("loading secrets: %w", err)
	}

	refs := make([]string, 0, len(cfg.Tools)*2)
	for _, t := range cfg.Tools {
		refs = append(refs, t.Auth, t.Endpoint)
	}
	if err := secProvider.Validate(refs); err != nil {
		return nil, nil, fmt.Errorf("config references undefined secrets: %w", err)
	}

	sc, err := scrubber.New(cfg.Scrubbing)
	if err != nil {
		return nil, nil, fmt.Errorf("building scrubber: %w", err)
	}

	limiters := make(map[string]*ratelimit.Limiter, len(cfg.Tools))
	for _, t := range cfg.Tools {
		limiters[t.Name] = ratelimit.New(t.Rules.MaxCallsPerMinute)
	}

	db, err := store.Open(filepath.Join(tskDir, "activity.db"))
	if err != nil {
		return nil, nil, fmt.Errorf("opening db: %w", err)
	}

	server := mcp.New(mcp.Config{
		Tools:        cfg.Tools,
		Exec:         proxy.New(secProvider),
		Scrubber:     sc,
		Limiters:     limiters,
		Log:          activitylog.New(db),
		Instructions: cfg.Instructions,
	})

	return server, func() {
		if err := db.Close(); err != nil {
			slog.Error("failed to close database", slog.Any("error", err))
		}
	}, nil
}

func runServer(tskDir string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	server, cleanup, err := buildServer(ctx, tskDir)
	if err != nil {
		return err
	}
	defer cleanup()

	return server.Serve(ctx, os.Stdin, os.Stdout)
}
