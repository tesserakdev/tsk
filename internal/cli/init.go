package cli

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize local tsk environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolving home directory: %w", err)
			}
			return runInit(filepath.Join(home, ".tsk"), cmd.OutOrStdout())
		},
	}
}

func runInit(dir string, w io.Writer) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	slog.Debug("directory ready", "path", dir)

	if err := writeIfNotExists(filepath.Join(dir, ".secrets"), defaultSecrets, 0600); err != nil {
		return err
	}
	if err := writeIfNotExists(filepath.Join(dir, "rules.yaml"), defaultRules, 0644); err != nil {
		return err
	}

	//nolint:errcheck
	fmt.Fprintf(w, "tsk initialized at %s\n", dir)
	//nolint:errcheck
	fmt.Fprintf(w, "  1. Add credentials to %s\n", filepath.Join(dir, ".secrets"))
	//nolint:errcheck
	fmt.Fprintf(w, "  2. Define tools in   %s\n", filepath.Join(dir, "rules.yaml"))
	//nolint:errcheck
	fmt.Fprintln(w, "  3. Run: tsk run")
	return nil
}

func writeIfNotExists(path, content string, perm os.FileMode) error {
	_, err := os.Stat(path)
	if err == nil {
		slog.Debug("file already exists, skipping", "path", path)
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	slog.Debug("created file", "path", path)
	return nil
}

const defaultSecrets = `# tsk secrets — never commit this file
# One KEY=value per line. Reference values in rules.yaml as ${KEY}.
#
# STRIPE_TEST_KEY=sk_test_...
# GITHUB_TOKEN=ghp_...
`

const defaultRules = `version: 1

# instructions is injected into the agent's system context on connect.
# Use it to tell the agent to prefer tsk tools over CLI alternatives.
# instructions: |
#   When a tsk tool covers an operation you need, use it instead of CLI
#   alternatives (gh, stripe, curl, etc.).

# tools defines which API calls your agent is allowed to make.
# tools: []

# scrubbing redacts sensitive patterns from API responses before they reach the agent.
# scrubbing:
#   - type: credit_card
#   - type: iban
#   - type: email
#   - type: ssn
`
