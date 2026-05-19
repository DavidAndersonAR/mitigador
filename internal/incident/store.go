// Package incident persists AttackEvents into the incidents and attack_updates tables.
//
// Filled in plan 01-08; see .planning/phases/01-observation-spine/01-08-PLAN.md.
package incident

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mitigador/mitigador/internal/detect"
)

// Incident is the in-memory representation of an incidents row.
type Incident struct {
	ID        string
	HostIP    netip.Addr
	Vector    detect.Vector
	Hostgroup string
	StartedAt time.Time
	EndedAt   *time.Time // nil = still active
	PeakPps   uint64
	PeakBps   uint64
	Score     float64
}

// Update is the in-memory representation of an attack_updates row.
type Update struct {
	IncidentID string
	ObservedAt time.Time
	Pps        uint64
	Bps        uint64
	Score      float64
	Kind       string // "started" | "update" | "ended"
}

// Filter narrows List results. All fields are optional.
type Filter struct {
	HostIP     *netip.Addr
	Vector     *detect.Vector
	Since      *time.Time
	Until      *time.Time
	ActiveOnly bool
	Limit      int // default 50, max 500
	Offset     int
}

// ListResult includes paginated items and total count.
type ListResult struct {
	Items []Incident
	Total int64
}

// ErrNotFound is returned by Get when no incident matches the given ID.
var ErrNotFound = errors.New("incident: not found")

// Store provides typed query methods over the incidents and attack_updates tables.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a new Store backed by the given pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Create inserts an incidents row and a 'started' attack_updates row atomically.
func (s *Store) Create(ctx context.Context, ev detect.AttackEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("incident: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	_, err = tx.Exec(ctx, `
		INSERT INTO incidents (id, host_ip, vector, hostgroup_id, started_at, peak_pps, peak_bps, score, details)
		VALUES ($1, $2::inet, $3, (SELECT id FROM hostgroups WHERE name=$4), $5, $6, $7, $8, '{}'::jsonb)
		ON CONFLICT (id) DO NOTHING
	`, ev.IncidentID, ev.HostIP.String(), string(ev.Vector), ev.Hostgroup,
		ev.StartedAt, int64(ev.PeakPps), int64(ev.PeakBps), ev.Confidence)
	if err != nil {
		return fmt.Errorf("incident: insert incidents: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO attack_updates (incident_id, observed_at, pps, bps, score, kind)
		VALUES ($1, $2, $3, $4, $5, 'started')
	`, ev.IncidentID, ev.Now, int64(ev.Pps), int64(ev.Bps), ev.Confidence)
	if err != nil {
		return fmt.Errorf("incident: insert attack_updates (started): %w", err)
	}

	return tx.Commit(ctx)
}

// Update inserts an 'update' attack_updates row and bumps peak_pps/bps in incidents if new peaks.
func (s *Store) Update(ctx context.Context, ev detect.AttackEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("incident: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	_, err = tx.Exec(ctx, `
		UPDATE incidents
		SET peak_pps = GREATEST(peak_pps, $2),
		    peak_bps = GREATEST(peak_bps, $3),
		    score    = $4
		WHERE id = $1
	`, ev.IncidentID, int64(ev.PeakPps), int64(ev.PeakBps), ev.Confidence)
	if err != nil {
		return fmt.Errorf("incident: update incidents: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO attack_updates (incident_id, observed_at, pps, bps, score, kind)
		VALUES ($1, $2, $3, $4, $5, 'update')
	`, ev.IncidentID, ev.Now, int64(ev.Pps), int64(ev.Bps), ev.Confidence)
	if err != nil {
		return fmt.Errorf("incident: insert attack_updates (update): %w", err)
	}

	return tx.Commit(ctx)
}

// End updates incidents.ended_at, bumps peaks, updates score, and inserts an 'ended' attack_updates row atomically.
func (s *Store) End(ctx context.Context, ev detect.AttackEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("incident: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	_, err = tx.Exec(ctx, `
		UPDATE incidents
		SET ended_at = $2,
		    peak_pps = GREATEST(peak_pps, $3),
		    peak_bps = GREATEST(peak_bps, $4),
		    score    = $5
		WHERE id = $1
	`, ev.IncidentID, ev.EndedAt, int64(ev.PeakPps), int64(ev.PeakBps), ev.Confidence)
	if err != nil {
		return fmt.Errorf("incident: end incidents: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO attack_updates (incident_id, observed_at, pps, bps, score, kind)
		VALUES ($1, $2, $3, $4, $5, 'ended')
	`, ev.IncidentID, ev.Now, int64(ev.Pps), int64(ev.Bps), ev.Confidence)
	if err != nil {
		return fmt.Errorf("incident: insert attack_updates (ended): %w", err)
	}

	return tx.Commit(ctx)
}

// List returns a paginated list of incidents matching the filter, ordered by started_at DESC.
func (s *Store) List(ctx context.Context, f Filter) (*ListResult, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	// Build WHERE clause with parameterized placeholders.
	where := "WHERE 1=1"
	args := []any{}
	idx := 1

	if f.HostIP != nil {
		where += fmt.Sprintf(" AND host_ip = $%d::inet", idx)
		args = append(args, f.HostIP.String())
		idx++
	}
	if f.Vector != nil {
		where += fmt.Sprintf(" AND vector = $%d", idx)
		args = append(args, string(*f.Vector))
		idx++
	}
	if f.Since != nil {
		where += fmt.Sprintf(" AND started_at >= $%d", idx)
		args = append(args, *f.Since)
		idx++
	}
	if f.Until != nil {
		where += fmt.Sprintf(" AND started_at < $%d", idx)
		args = append(args, *f.Until)
		idx++
	}
	if f.ActiveOnly {
		where += " AND ended_at IS NULL"
	}

	var total int64
	if err := s.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM incidents "+where, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("incident: list count: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT i.id, host_ip::text, vector, COALESCE(h.name, ''), started_at, ended_at, peak_pps, peak_bps, score
		FROM incidents i LEFT JOIN hostgroups h ON h.id = i.hostgroup_id
		%s
		ORDER BY started_at DESC
		LIMIT $%d OFFSET $%d
	`, where, idx, idx+1)
	rows, err := s.pool.Query(ctx, query, append(args, limit, f.Offset)...)
	if err != nil {
		return nil, fmt.Errorf("incident: list query: %w", err)
	}
	defer rows.Close()

	var items []Incident
	for rows.Next() {
		var inc Incident
		var hostIPStr string
		var vec string
		if err := rows.Scan(&inc.ID, &hostIPStr, &vec, &inc.Hostgroup,
			&inc.StartedAt, &inc.EndedAt, &inc.PeakPps, &inc.PeakBps, &inc.Score); err != nil {
			return nil, fmt.Errorf("incident: list scan: %w", err)
		}
		inc.HostIP = stripCIDRSuffix(hostIPStr)
		inc.Vector = detect.Vector(vec)
		items = append(items, inc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("incident: list rows: %w", err)
	}

	return &ListResult{Items: items, Total: total}, nil
}

// Get returns the incident and its ordered attack_updates. Returns ErrNotFound if not found.
func (s *Store) Get(ctx context.Context, id string) (*Incident, []Update, error) {
	inc := &Incident{}
	var hostIPStr, vec string

	err := s.pool.QueryRow(ctx, `
		SELECT i.id, host_ip::text, vector, COALESCE(h.name, ''), started_at, ended_at, peak_pps, peak_bps, score
		FROM incidents i LEFT JOIN hostgroups h ON h.id = i.hostgroup_id
		WHERE i.id = $1
	`, id).Scan(&inc.ID, &hostIPStr, &vec, &inc.Hostgroup,
		&inc.StartedAt, &inc.EndedAt, &inc.PeakPps, &inc.PeakBps, &inc.Score)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("incident: get: %w", err)
	}

	inc.HostIP = stripCIDRSuffix(hostIPStr)
	inc.Vector = detect.Vector(vec)

	rows, err := s.pool.Query(ctx, `
		SELECT incident_id, observed_at, pps, bps, score, kind
		FROM attack_updates
		WHERE incident_id = $1
		ORDER BY observed_at
	`, id)
	if err != nil {
		return nil, nil, fmt.Errorf("incident: get updates: %w", err)
	}
	defer rows.Close()

	var updates []Update
	for rows.Next() {
		var u Update
		if err := rows.Scan(&u.IncidentID, &u.ObservedAt, &u.Pps, &u.Bps, &u.Score, &u.Kind); err != nil {
			return nil, nil, fmt.Errorf("incident: get updates scan: %w", err)
		}
		updates = append(updates, u)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("incident: get updates rows: %w", err)
	}

	return inc, updates, nil
}

// ListActive returns all incidents with ended_at IS NULL (limit 500).
func (s *Store) ListActive(ctx context.Context) ([]Incident, error) {
	result, err := s.List(ctx, Filter{ActiveOnly: true, Limit: 500})
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

// CloseOrphans marks any incident with ended_at IS NULL and created_at < cutoff
// as ended_at = cutoff. Called at serve startup for crash recovery.
// Returns the number of rows affected.
func (s *Store) CloseOrphans(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE incidents
		SET ended_at = $1
		WHERE ended_at IS NULL AND created_at < $1
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("incident: close orphans: %w", err)
	}
	return tag.RowsAffected(), nil
}

// stripCIDRSuffix removes any trailing /prefix-len from an INET string returned by Postgres.
// e.g. "10.0.0.1/32" → "10.0.0.1", "192.168.1.1" → "192.168.1.1".
func stripCIDRSuffix(s string) netip.Addr {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			s = s[:i]
			break
		}
	}
	addr, _ := netip.ParseAddr(s)
	return addr
}
