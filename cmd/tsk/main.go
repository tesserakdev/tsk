package main

import (
	"log/slog"
	"os"

	"github.com/tesserakdev/tsk/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		slog.Error("failed to execute root command", slog.Any("error", err))
		os.Exit(1)
	}
}
