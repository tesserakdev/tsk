package cli

import (
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/tesserakdev/tsk/internal/activitylog"
	"github.com/tesserakdev/tsk/internal/store"
)

func newLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Query the local activity log",
		RunE: func(cmd *cobra.Command, args []string) error {
			tail, _ := cmd.Flags().GetInt("tail")
			tool, _ := cmd.Flags().GetString("tool")
			since, _ := cmd.Flags().GetDuration("since")
			dir, _ := cmd.Flags().GetString("dir")
			tskDir, err := resolveTskDir(dir)
			if err != nil {
				return err
			}
			return runLogs(tskDir, tail, tool, since, cmd.OutOrStdout())
		},
	}

	cmd.Flags().Int("tail", 0, "Show last N entries")
	cmd.Flags().String("tool", "", "Filter by tool name")
	cmd.Flags().Duration("since", 0, "Filter by time range (e.g. 1h, 30m)")
	cmd.Flags().String("dir", "~/.tsk", "tsk directory (overrides default ~/.tsk)")

	return cmd
}

func runLogs(tskDir string, tail int, tool string, since time.Duration, out io.Writer) error {
	dbPath := filepath.Join(tskDir, "activity.db")

	db, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening db: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("failed to close database", slog.Any("error", err))
		}
	}()

	entries, err := activitylog.New(db).Query(tool, tail, since)
	if err != nil {
		return fmt.Errorf("querying activity log: %w", err)
	}

	if len(entries) == 0 {
		//nolint:errcheck
		fmt.Fprintln(out, "No entries found.")
		return nil
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	//nolint:errcheck
	fmt.Fprintln(w, "ID\tTIME\tTOOL\tSTATUS\tSCRUBS\tRESPONSE")
	//nolint:errcheck
	fmt.Fprintln(w, "--\t----\t----\t------\t------\t--------")
	for _, e := range entries {
		preview := e.Response
		if len(preview) > 80 {
			preview = preview[:80] + "…"
		}
		//nolint:errcheck
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\n",
			e.ID, e.TS.Local().Format(time.RFC3339), e.Tool, e.Status, e.ScrubActions, preview,
		)
	}
	return w.Flush()
}
