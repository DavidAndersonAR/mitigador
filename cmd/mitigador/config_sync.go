package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/mitigador/mitigador/internal/config"
	pg "github.com/mitigador/mitigador/internal/storage/postgres"
)

// newConfigCmd builds the `mitigador config` subtree.
// The `sync` subcommand is the primary action — it upserts domain tables from a YAML file.
func newConfigCmd(configPath *string) *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Manage domain configuration",
	}
	c.AddCommand(newConfigSyncCmd(configPath))
	return c
}

func newConfigSyncCmd(configPath *string) *cobra.Command {
	var domainPath string
	c := &cobra.Command{
		Use:   "sync",
		Short: "Sync domain config (exporters, hostgroups, thresholds, alert_channels, whitelist) from YAML into Postgres",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(*configPath)
			if err != nil {
				return err
			}
			d, err := config.LoadDomain(domainPath)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()
			if err := pg.Migrate(cfg.Postgres.DSN); err != nil {
				return fmt.Errorf("migrate: %w", err)
			}
			pool, err := pg.NewPool(ctx, cfg.Postgres.DSN, 4, 1)
			if err != nil {
				return err
			}
			defer pool.Close()
			diff, err := config.Sync(ctx, pool, d)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "exporters:      added=%d updated=%d unchanged=%d\n",
				diff.Exporters.Added, diff.Exporters.Updated, diff.Exporters.Unchanged)
			fmt.Fprintf(cmd.OutOrStdout(), "hostgroups:     added=%d updated=%d unchanged=%d\n",
				diff.Hostgroups.Added, diff.Hostgroups.Updated, diff.Hostgroups.Unchanged)
			fmt.Fprintf(cmd.OutOrStdout(), "thresholds:     added=%d updated=%d unchanged=%d\n",
				diff.Thresholds.Added, diff.Thresholds.Updated, diff.Thresholds.Unchanged)
			fmt.Fprintf(cmd.OutOrStdout(), "alert_channels: added=%d updated=%d unchanged=%d\n",
				diff.AlertChannels.Added, diff.AlertChannels.Updated, diff.AlertChannels.Unchanged)
			fmt.Fprintf(cmd.OutOrStdout(), "whitelist:      added=%d updated=%d unchanged=%d\n",
				diff.Whitelist.Added, diff.Whitelist.Updated, diff.Whitelist.Unchanged)
			return nil
		},
	}
	c.Flags().StringVar(&domainPath, "file", "", "path to domain.yaml (required)")
	_ = c.MarkFlagRequired("file")
	return c
}
