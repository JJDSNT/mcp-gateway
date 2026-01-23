// internal/cli/stdio.go
package cli

import (
	"github.com/spf13/cobra"

	"mcp-router/internal/app"
)

func newStdioCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stdio",
		Short: "Run MCP gateway in stdio mode (default)",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := app.New(cfgPath)
			if err != nil {
				return err
			}
			return a.RunStdio(cmd.Context())
		},
	}
	return cmd
}
