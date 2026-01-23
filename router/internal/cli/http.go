// internal/cli/http.go
package cli

import (
	"context"
	"fmt"
	"log"

	"github.com/spf13/cobra"

	"mcp-router/internal/app"
)

func newHTTPCmd() *cobra.Command {
	var (
		addr      string
		alsoStdio bool
	)

	cmd := &cobra.Command{
		Use:   "http",
		Short: "Run MCP gateway in HTTP mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			if addr == "" {
				return fmt.Errorf("missing required flag: --addr (e.g. --addr :8080)")
			}

			// allow cancel when stdio goroutine fails
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			a, err := app.New(cfgPath)
			if err != nil {
				return err
			}

			if alsoStdio {
				go func() {
					if err := a.RunStdio(ctx); err != nil {
						log.Printf("stdio error: %v", err)
						cancel()
					}
				}()
			}

			return a.RunHTTP(ctx, addr)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "", "HTTP listen address (e.g. :8080)")
	cmd.Flags().BoolVar(&alsoStdio, "also-stdio", false, "also run stdio while HTTP is running")

	return cmd
}
