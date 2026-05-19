package config

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"
)

// Domain is the optional declarative YAML for domain tables.
// Kept SEPARATE from infra Config (which lives in config.go) — D-07.
type Domain struct {
	Exporters     []ExporterEntry     `yaml:"exporters"`
	Hostgroups    []HostgroupEntry    `yaml:"hostgroups"`
	Thresholds    []ThresholdEntry    `yaml:"thresholds"`
	AlertChannels []AlertChannelEntry `yaml:"alert_channels"`
	Whitelist     []WhitelistEntry    `yaml:"whitelist"`
}

// ExporterEntry represents one row in the exporters table.
type ExporterEntry struct {
	SourceIP           string `yaml:"source_ip"`
	Type               string `yaml:"type"`
	SampleRateOverride int    `yaml:"sample_rate_override"`
	Description        string `yaml:"description"`
}

// HostgroupEntry represents one row in the hostgroups table.
type HostgroupEntry struct {
	Name        string `yaml:"name"`
	Prefix      string `yaml:"prefix"`
	Description string `yaml:"description"`
}

// ThresholdEntry represents one row in the thresholds table.
// Hostgroup is a name that resolves to hostgroups.id.
type ThresholdEntry struct {
	Hostgroup    string `yaml:"hostgroup"`
	Vector       string `yaml:"vector"`
	PPS          int64  `yaml:"pps"`
	BPS          int64  `yaml:"bps"`
	MinWindowSec int    `yaml:"min_window_sec"`
	GraceSec     int    `yaml:"grace_sec"`
}

// AlertChannelEntry represents one row in the alert_channels table.
type AlertChannelEntry struct {
	Type    string `yaml:"type"`
	Target  string `yaml:"target"`
	Enabled bool   `yaml:"enabled"`
}

// WhitelistEntry represents one row in the whitelist table.
type WhitelistEntry struct {
	Prefix string `yaml:"prefix"`
	Reason string `yaml:"reason"`
}

// SyncCounts tracks how many rows were added, updated, or unchanged for one table.
type SyncCounts struct {
	Added     int
	Updated   int
	Unchanged int
}

// SyncDiff is the result of a Sync call — rows added/updated/unchanged per table.
type SyncDiff struct {
	Exporters     SyncCounts
	Hostgroups    SyncCounts
	Thresholds    SyncCounts
	AlertChannels SyncCounts
	Whitelist     SyncCounts
}

// LoadDomain reads a Domain from a YAML file.
func LoadDomain(path string) (*Domain, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("domain: read %s: %w", path, err)
	}
	var d Domain
	if err := yaml.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("domain: unmarshal: %w", err)
	}
	return &d, nil
}

// Sync upserts the Domain into Postgres and returns a SyncDiff summary.
//
// Rows present in the DB but absent from the YAML are LEFT ALONE — no deletion
// occurs in Phase 1. Deletion is handled by the Phase 3 CRUD UI.
//
// D-10: No default threshold templates are seeded anywhere in this function.
// If the YAML omits a threshold for a hostgroup, that hostgroup simply has no rule.
func Sync(ctx context.Context, pool *pgxpool.Pool, d *Domain) (*SyncDiff, error) {
	if d == nil {
		return nil, errors.New("domain: nil domain")
	}
	diff := &SyncDiff{}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("domain: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// ── Exporters: upsert by source_ip ──────────────────────────────────────
	// We SELECT before upsert to detect add vs update vs unchanged for the diff.
	for _, e := range d.Exporters {
		var existingType string
		var existingSampleRate int
		var existingDesc string
		selectErr := tx.QueryRow(ctx,
			`SELECT type, sample_rate_override, COALESCE(description,'') FROM exporters WHERE source_ip=$1::inet`,
			e.SourceIP,
		).Scan(&existingType, &existingSampleRate, &existingDesc)

		_, upsertErr := tx.Exec(ctx, `
			INSERT INTO exporters (source_ip, type, sample_rate_override, description)
			VALUES ($1::inet, $2, $3, $4)
			ON CONFLICT (source_ip) DO UPDATE
			  SET type                 = EXCLUDED.type,
			      sample_rate_override = EXCLUDED.sample_rate_override,
			      description          = EXCLUDED.description,
			      updated_at           = now()`,
			e.SourceIP, e.Type, e.SampleRateOverride, e.Description,
		)
		if upsertErr != nil {
			return nil, fmt.Errorf("domain: upsert exporter %s: %w", e.SourceIP, upsertErr)
		}

		if errors.Is(selectErr, pgx.ErrNoRows) {
			diff.Exporters.Added++
		} else if selectErr != nil {
			return nil, fmt.Errorf("domain: select exporter %s: %w", e.SourceIP, selectErr)
		} else {
			changed := existingType != e.Type ||
				existingSampleRate != e.SampleRateOverride ||
				existingDesc != e.Description
			if changed {
				diff.Exporters.Updated++
			} else {
				diff.Exporters.Unchanged++
			}
		}
	}

	// ── Hostgroups: upsert by name ───────────────────────────────────────────
	hostgroupIDs := make(map[string]int64)
	for _, h := range d.Hostgroups {
		var id int64
		var existingDesc string
		selectErr := tx.QueryRow(ctx,
			`SELECT id, COALESCE(description,'') FROM hostgroups WHERE name=$1`,
			h.Name,
		).Scan(&id, &existingDesc)

		var upsertID int64
		upsertErr := tx.QueryRow(ctx, `
			INSERT INTO hostgroups (name, prefix, description)
			VALUES ($1, $2::cidr, $3)
			ON CONFLICT (name) DO UPDATE
			  SET prefix      = EXCLUDED.prefix,
			      description = EXCLUDED.description
			RETURNING id`,
			h.Name, h.Prefix, h.Description,
		).Scan(&upsertID)
		if upsertErr != nil {
			return nil, fmt.Errorf("domain: upsert hostgroup %s: %w", h.Name, upsertErr)
		}

		if errors.Is(selectErr, pgx.ErrNoRows) {
			diff.Hostgroups.Added++
			hostgroupIDs[h.Name] = upsertID
		} else if selectErr != nil {
			return nil, fmt.Errorf("domain: select hostgroup %s: %w", h.Name, selectErr)
		} else {
			hostgroupIDs[h.Name] = id
			if existingDesc != h.Description {
				diff.Hostgroups.Updated++
			} else {
				diff.Hostgroups.Unchanged++
			}
		}
	}

	// ── Thresholds: upsert by (hostgroup_id, vector) ─────────────────────────
	for _, t := range d.Thresholds {
		hgID, ok := hostgroupIDs[t.Hostgroup]
		if !ok {
			// Hostgroup exists in DB but was not in this YAML batch.
			if err := tx.QueryRow(ctx,
				`SELECT id FROM hostgroups WHERE name=$1`, t.Hostgroup,
			).Scan(&hgID); err != nil {
				return nil, fmt.Errorf("domain: threshold references unknown hostgroup %q: %w", t.Hostgroup, err)
			}
		}

		var existingPPS, existingBPS int64
		var existingWindow, existingGrace int
		selectErr := tx.QueryRow(ctx,
			`SELECT pps, bps, min_window_sec, grace_sec FROM thresholds WHERE hostgroup_id=$1 AND vector=$2`,
			hgID, t.Vector,
		).Scan(&existingPPS, &existingBPS, &existingWindow, &existingGrace)

		_, upsertErr := tx.Exec(ctx, `
			INSERT INTO thresholds (hostgroup_id, vector, pps, bps, min_window_sec, grace_sec)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (hostgroup_id, vector) DO UPDATE
			  SET pps            = EXCLUDED.pps,
			      bps            = EXCLUDED.bps,
			      min_window_sec = EXCLUDED.min_window_sec,
			      grace_sec      = EXCLUDED.grace_sec`,
			hgID, t.Vector, t.PPS, t.BPS, t.MinWindowSec, t.GraceSec,
		)
		if upsertErr != nil {
			return nil, fmt.Errorf("domain: upsert threshold %s/%s: %w", t.Hostgroup, t.Vector, upsertErr)
		}

		if errors.Is(selectErr, pgx.ErrNoRows) {
			diff.Thresholds.Added++
		} else if selectErr != nil {
			return nil, fmt.Errorf("domain: select threshold %s/%s: %w", t.Hostgroup, t.Vector, selectErr)
		} else {
			changed := existingPPS != t.PPS || existingBPS != t.BPS ||
				existingWindow != t.MinWindowSec || existingGrace != t.GraceSec
			if changed {
				diff.Thresholds.Updated++
			} else {
				diff.Thresholds.Unchanged++
			}
		}
	}

	// ── Alert channels: upsert by (type, target) ────────────────────────────
	for _, ch := range d.AlertChannels {
		var existingEnabled bool
		selectErr := tx.QueryRow(ctx,
			`SELECT enabled FROM alert_channels WHERE type=$1 AND target=$2`,
			ch.Type, ch.Target,
		).Scan(&existingEnabled)

		_, upsertErr := tx.Exec(ctx, `
			INSERT INTO alert_channels (type, target, enabled)
			VALUES ($1, $2, $3)
			ON CONFLICT (type, target) DO UPDATE
			  SET enabled = EXCLUDED.enabled`,
			ch.Type, ch.Target, ch.Enabled,
		)
		if upsertErr != nil {
			return nil, fmt.Errorf("domain: upsert alert_channel %s/%s: %w", ch.Type, ch.Target, upsertErr)
		}

		if errors.Is(selectErr, pgx.ErrNoRows) {
			diff.AlertChannels.Added++
		} else if selectErr != nil {
			return nil, fmt.Errorf("domain: select alert_channel %s/%s: %w", ch.Type, ch.Target, selectErr)
		} else {
			if existingEnabled != ch.Enabled {
				diff.AlertChannels.Updated++
			} else {
				diff.AlertChannels.Unchanged++
			}
		}
	}

	// ── Whitelist: upsert by prefix ──────────────────────────────────────────
	for _, w := range d.Whitelist {
		var existingReason string
		selectErr := tx.QueryRow(ctx,
			`SELECT COALESCE(reason,'') FROM whitelist WHERE prefix=$1::cidr`,
			w.Prefix,
		).Scan(&existingReason)

		_, upsertErr := tx.Exec(ctx, `
			INSERT INTO whitelist (prefix, reason)
			VALUES ($1::cidr, $2)
			ON CONFLICT (prefix) DO UPDATE
			  SET reason = EXCLUDED.reason`,
			w.Prefix, w.Reason,
		)
		if upsertErr != nil {
			return nil, fmt.Errorf("domain: upsert whitelist %s: %w", w.Prefix, upsertErr)
		}

		if errors.Is(selectErr, pgx.ErrNoRows) {
			diff.Whitelist.Added++
		} else if selectErr != nil {
			return nil, fmt.Errorf("domain: select whitelist %s: %w", w.Prefix, selectErr)
		} else {
			if existingReason != w.Reason {
				diff.Whitelist.Updated++
			} else {
				diff.Whitelist.Unchanged++
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("domain: commit: %w", err)
	}
	return diff, nil
}
