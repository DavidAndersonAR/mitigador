package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mitigador/mitigador/internal/config"
	"github.com/mitigador/mitigador/internal/version"
)

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:     "mitigador",
		Short:   "DDoS detection and mitigation for ISPs",
		Long:    "Mitigador detecta e mitiga ataques DDoS volumétricos para ISPs via NetFlow/IPFIX/sFlow + BGP RTBH/Flowspec. Phase 1 entrega observação pura.",
		Version: version.String(),
	}
	cmd.PersistentFlags().StringVar(&configPath, "config", config.DefaultPath, "path to mitigador config.yaml")
	cmd.AddCommand(
		newVersionCmd(),
		newServeCmd(&configPath),
		newConfigCmd(&configPath),
		newUserCmd(&configPath),
	)
	return cmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.String())
		},
	}
}

// newConfigCmd is defined in config_sync.go (plan 01-04 Task 3).
// This declaration lives here so main.go compiles independently of task ordering.
// The actual implementation is in cmd/mitigador/config_sync.go.
