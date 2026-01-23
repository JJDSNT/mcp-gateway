// internal/cli/config_show.go
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Config utilities",
	}

	cmd.AddCommand(newConfigShowCmd())
	return cmd
}

func newConfigShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print resolved config path and file contents",
		RunE: func(cmd *cobra.Command, args []string) error {
			return printConfig(cfgPath)
		},
	}
	return cmd
}

func printConfig(path string) error {
	abs := path
	if p, err := filepath.Abs(path); err == nil {
		abs = p
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("unable to read config file %q (resolved: %q): %w", path, abs, err)
	}

	fmt.Printf("config.path=%s\n", abs)
	fmt.Printf("config.bytes=%d\n", len(b))
	fmt.Printf("----- BEGIN CONFIG (%s) -----\n", abs)
	_, _ = os.Stdout.Write(b)
	if len(b) == 0 || b[len(b)-1] != '\n' {
		fmt.Println()
	}
	fmt.Printf("----- END CONFIG (%s) -----\n", abs)
	return nil
}
