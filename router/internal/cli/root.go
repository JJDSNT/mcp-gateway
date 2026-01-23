// internal/cli/root.go
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"mcp-router/internal/app"
)

var (
	// build info (inject via -ldflags)
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"

	// global flags
	cfgPath string
	verbose bool
	quiet   bool
)

// NewRootCmd builds the root command for mcp-gw.
//
// Behavior:
// - default config: ./config/config.yaml if exists, else /config/config.yaml
// - running with no subcommand defaults to "stdio"
func NewRootCmd() *cobra.Command {
	defaultConfig := pickDefaultConfig()

	cmd := &cobra.Command{
		Use:   "mcp-gw",
		Short: "mcp-gw (mcp-router gateway)",
		Long:  "mcp-gw is a gateway for routing MCP traffic via stdio and/or HTTP.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// default behavior: stdio
			return runStdioDefault(cmd.Context(), cfgPath)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// global flags
	cmd.PersistentFlags().StringVar(
		&cfgPath,
		"config",
		defaultConfig,
		"path to config.yaml",
	)
	cmd.PersistentFlags().BoolVar(
		&verbose,
		"verbose",
		false,
		"enable verbose logging",
	)
	cmd.PersistentFlags().BoolVar(
		&quiet,
		"quiet",
		false,
		"suppress non-error logs",
	)

	// version wiring (supports `mcp-gw --version`)
	cmd.Version = Version
	cmd.SetVersionTemplate(versionTemplate())

	// register subcommands
	cmd.AddCommand(
		newStdioCmd(),
		newHTTPCmd(),
		newConfigCmd(),
		newVersionCmd(),
	)

	return cmd
}

// Execute is called by cmd/mcp-gw/main.go
func Execute() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	root := NewRootCmd()
	root.SetContext(ctx)

	if err := root.Execute(); err != nil {
		// Cobra output is silenced; print clean error
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func runStdioDefault(ctx context.Context, configPath string) error {
	a, err := app.New(configPath)
	if err != nil {
		return err
	}
	return a.RunStdio(ctx)
}

// pickDefaultConfig tries to make local/dev and docker usage easy:
// - if ./config/config.yaml exists (repo root), use it
// - else fall back to /config/config.yaml (Docker)
func pickDefaultConfig() string {
	candidate := filepath.Join(".", "config", "config.yaml")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return "/config/config.yaml"
}

func versionTemplate() string {
	return `mcp-gw {{.Version}}
`
}
