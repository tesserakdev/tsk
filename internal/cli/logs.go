package cli

import (
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
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
			eventType, _ := cmd.Flags().GetString("type")
			tskDir, err := resolveTskDir(dir)
			if err != nil {
				return err
			}
			return runLogs(tskDir, tail, tool, since, eventType, cmd.OutOrStdout())
		},
	}

	cmd.Flags().Int("tail", 0, "Show last N entries")
	cmd.Flags().String("tool", "", "Filter requests by tool name")
	cmd.Flags().Duration("since", 0, "Filter by time range (e.g. 1h, 30m)")
	cmd.Flags().String("dir", "~/.tsk", "tsk directory (overrides default ~/.tsk)")
	cmd.Flags().String("type", "", `Filter by event type: "request" or "rotation" (default: all)`)

	return cmd
}

func runLogs(tskDir string, tail int, tool string, since time.Duration, eventType string, out io.Writer) error {
	if eventType != "" && eventType != "request" && eventType != "rotation" {
		return fmt.Errorf("unknown --type value %q: must be \"request\" or \"rotation\"", eventType)
	}

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

	events, err := activitylog.New(db).QueryEvents(tool, tail, since, eventType)
	if err != nil {
		return fmt.Errorf("querying activity log: %w", err)
	}

	if len(events) == 0 {
		//nolint:errcheck
		fmt.Fprintln(out, "No entries found.")
		return nil
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, e := range events {
		//nolint:errcheck
		fmt.Fprintf(w, "%s\t%s\t%s\n",
			e.TS().Local().Format("2006-01-02 15:04:05"),
			e.Kind(),
			eventDetail(e),
		)
	}
	return w.Flush()
}

func eventDetail(e activitylog.Event) string {
	if e.Rotation != nil {
		return strings.Join(e.Rotation.Keys, ", ")
	}
	preview := e.Request.Response
	if len(preview) > 80 {
		preview = preview[:80] + "…"
	}
	detail := fmt.Sprintf("%s\t%d\t%s", e.Request.Tool, e.Request.Status, preview)
	if e.Request.ScrubActions > 0 {
		detail += fmt.Sprintf("\t[%d scrubs]", e.Request.ScrubActions)
	}
	return detail
}
