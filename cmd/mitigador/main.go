package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mitigador/mitigador/internal/version"
)

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "mitigador",
		Short:   "DDoS detection and mitigation for ISPs",
		Long:    "Mitigador detecta e mitiga ataques DDoS volumétricos para ISPs via NetFlow/IPFIX/sFlow + BGP RTBH/Flowspec. Phase 1 entrega observação pura.",
		Version: version.String(),
	}
	cmd.AddCommand(
		newVersionCmd(),
		newServeStubCmd(),
		newConfigCmd(),
		newUserCmd(),
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

func newServeStubCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the Mitigador daemon (HTTP API + UDP listeners + detection)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("serve: not yet implemented (see plan 01-12)")
		},
	}
}

func newConfigCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Manage domain configuration",
	}
	c.AddCommand(&cobra.Command{
		Use:   "sync",
		Short: "Sync domain config (exporters, hostgroups, thresholds, alert channels, whitelist) from YAML into Postgres",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("config sync: not yet implemented (see plan 01-04)")
		},
	})
	return c
}

func newUserCmd() *cobra.Command {
	u := &cobra.Command{
		Use:   "user",
		Short: "Manage dashboard users",
	}
	for _, sub := range []string{"create", "list", "passwd", "delete"} {
		sub := sub
		u.AddCommand(&cobra.Command{
			Use:   sub,
			Short: "user " + sub + " (stub — see plan 01-04)",
			RunE: func(cmd *cobra.Command, args []string) error {
				return fmt.Errorf("user %s: not yet implemented (see plan 01-04)", sub)
			},
		})
	}
	return u
}
