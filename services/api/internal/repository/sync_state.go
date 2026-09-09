package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type syncStateRepository struct {
	pool pgxPool
}

// NewSyncStateRepository constructs the Postgres-backed SyncStateRepository.
func NewSyncStateRepository(pool pgxPool) domain.SyncStateRepository {
	return &syncStateRepository{pool: pool}
}

// syncStateColumns is the canonical projection shared by every sync_state SELECT
// so scanSyncState stays in lockstep with the query column order.
const syncStateColumns = `id, business_id, platform, external_id, last_checked_at,
	last_remote_snapshot, drift_detected, drift_fields, consecutive_failures,
	last_error, next_check_at, created_at, updated_at`

// scanSyncState maps one sync_state row into a domain.SyncState. The JSONB
// snapshot column scans into a []byte then unmarshals into the string map; a
// SQL NULL / empty object yields an empty map.
func scanSyncState(row scanner) (domain.SyncState, error) {
	var s domain.SyncState
	var snapshotRaw []byte
	if err := row.Scan(
		&s.ID,
		&s.BusinessID,
		&s.Platform,
		&s.ExternalID,
		&s.LastCheckedAt,
		&snapshotRaw,
		&s.DriftDetected,
		&s.DriftFields,
		&s.ConsecutiveFailures,
		&s.LastError,
		&s.NextCheckAt,
		&s.CreatedAt,
		&s.UpdatedAt,
	); err != nil {
		return s, err
	}
	if len(snapshotRaw) > 0 {
		if err := json.Unmarshal(snapshotRaw, &s.LastRemoteSnapshot); err != nil {
			return s, fmt.Errorf("unmarshal snapshot: %w", err)
		}
	}
	if s.LastRemoteSnapshot == nil {
		s.LastRemoteSnapshot = map[string]string{}
	}
	return s, nil
}

func (r *syncStateRepository) UpsertPending(ctx context.Context, businessID uuid.UUID, platform, externalID string) error {
	const q = `
		INSERT INTO sync_state (business_id, platform, external_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (business_id, platform, external_id) DO NOTHING`
	if _, err := r.pool.Exec(ctx, q, businessID, platform, externalID); err != nil {
		return fmt.Errorf("upsert sync_state: %w", err)
	}
	return nil
}

// ListDue selects rows past their next_check_at that still map to an active,
// non-deleted integration. The JOIN is the gate that keeps the reconciler from
// polling revoked/expired tokens: a MarkTokenExpired flip or a disconnect drops
// the row from this result set without any sync_state mutation. FOR UPDATE OF s
// SKIP LOCKED only de-dups pollers that run this query at the same instant — the
// lock releases when the autocommit SELECT returns, not across the subsequent
// reconcile+MarkChecked. Exactly-once is not guaranteed under concurrent
// pollers; final de-dup relies on MarkChecked advancing next_check_at plus
// reconciliation being idempotent (a duplicate pass recomputes the same drift).
func (r *syncStateRepository) ListDue(ctx context.Context, now time.Time, limit int) ([]domain.SyncState, error) {
	q := `
		SELECT ` + prefixColumns("s") + `
		FROM sync_state s
		JOIN integrations i
		  ON i.business_id = s.business_id
		 AND i.platform = s.platform
		 AND i.external_id = s.external_id
		 AND i.status = 'active'
		 AND i.deleted_at IS NULL
		WHERE s.next_check_at <= $1
		ORDER BY s.next_check_at
		LIMIT $2
		FOR UPDATE OF s SKIP LOCKED`
	rows, err := r.pool.Query(ctx, q, now, limit)
	if err != nil {
		return nil, fmt.Errorf("query due sync_state: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.SyncState, error) {
		return scanSyncState(row)
	})
}

func (r *syncStateRepository) ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.SyncState, error) {
	q := `SELECT ` + syncStateColumns + ` FROM sync_state WHERE business_id = $1 ORDER BY platform, external_id`
	rows, err := r.pool.Query(ctx, q, businessID)
	if err != nil {
		return nil, fmt.Errorf("query sync_state by business: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.SyncState, error) {
		return scanSyncState(row)
	})
}

func (r *syncStateRepository) MarkChecked(ctx context.Context, id uuid.UUID, snapshot map[string]string, driftFields []string, checkedAt, nextCheckAt time.Time) error {
	if snapshot == nil {
		snapshot = map[string]string{}
	}
	if driftFields == nil {
		driftFields = []string{}
	}
	snapshotRaw, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	const q = `
		UPDATE sync_state
		SET last_remote_snapshot = $2,
		    drift_detected = $3,
		    drift_fields = $4,
		    consecutive_failures = 0,
		    last_error = '',
		    last_checked_at = $5,
		    next_check_at = $6,
		    updated_at = now()
		WHERE id = $1`
	if _, err := r.pool.Exec(ctx, q, id, snapshotRaw, len(driftFields) > 0, driftFields, checkedAt, nextCheckAt); err != nil {
		return fmt.Errorf("mark sync_state checked: %w", err)
	}
	return nil
}

func (r *syncStateRepository) MarkFailure(ctx context.Context, id uuid.UUID, lastError string, checkedAt, nextCheckAt time.Time) error {
	const q = `
		UPDATE sync_state
		SET consecutive_failures = consecutive_failures + 1,
		    last_error = $2,
		    last_checked_at = $3,
		    next_check_at = $4,
		    updated_at = now()
		WHERE id = $1`
	if _, err := r.pool.Exec(ctx, q, id, lastError, checkedAt, nextCheckAt); err != nil {
		return fmt.Errorf("mark sync_state failure: %w", err)
	}
	return nil
}

func (r *syncStateRepository) ScheduleImmediate(ctx context.Context, businessID uuid.UUID) error {
	const q = `UPDATE sync_state SET next_check_at = now(), updated_at = now() WHERE business_id = $1`
	if _, err := r.pool.Exec(ctx, q, businessID); err != nil {
		return fmt.Errorf("schedule sync_state immediate: %w", err)
	}
	return nil
}

// prefixColumns returns syncStateColumns with each column aliased to the given
// table alias, for the JOIN query where "id" et al. would otherwise be
// ambiguous against the integrations columns.
func prefixColumns(alias string) string {
	cols := []string{
		"id", "business_id", "platform", "external_id", "last_checked_at",
		"last_remote_snapshot", "drift_detected", "drift_fields", "consecutive_failures",
		"last_error", "next_check_at", "created_at", "updated_at",
	}
	for i, c := range cols {
		cols[i] = alias + "." + c
	}
	out := ""
	for i, c := range cols {
		if i > 0 {
			out += ", "
		}
		out += c
	}
	return out
}

// WithReconcileLock holds a transaction-scoped advisory lock on one pinned
// connection for the complete pass. false means another process owns the pass.
func (r *syncStateRepository) WithReconcileLock(ctx context.Context, fn func() error) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin reconcile lock: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtext('onevoice.sync-reconcile'))").Scan(&locked); err != nil {
		return false, fmt.Errorf("acquire reconcile lock: %w", err)
	}
	if !locked {
		return false, nil
	}
	if err := fn(); err != nil {
		return true, err
	}
	if err := tx.Commit(ctx); err != nil {
		return true, fmt.Errorf("commit reconcile lock: %w", err)
	}
	return true, nil
}

// MarkCheckedEpisode atomically records the snapshot and creates one episode
// on a false-to-true transition. A matching snapshot clears the episode.
func (r *syncStateRepository) MarkCheckedEpisode(ctx context.Context, id uuid.UUID, snapshot map[string]string, driftFields []string, checkedAt, nextCheckAt time.Time) (domain.DriftAlertEpisode, error) {
	if snapshot == nil {
		snapshot = map[string]string{}
	}
	if driftFields == nil {
		driftFields = []string{}
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return domain.DriftAlertEpisode{}, fmt.Errorf("marshal snapshot: %w", err)
	}
	candidate := uuid.New()
	const q = `
		UPDATE sync_state
		SET last_remote_snapshot=$2, drift_detected=$3, drift_fields=$4,
		    consecutive_failures=0, last_error='', last_checked_at=$5,
		    next_check_at=$6, updated_at=now(),
		    drift_episode_id=CASE
		      WHEN NOT $3 THEN NULL
		      WHEN NOT drift_detected OR drift_episode_id IS NULL THEN $7
		      ELSE drift_episode_id END,
		    drift_task_created_at=CASE
		      WHEN NOT $3 OR NOT drift_detected OR drift_episode_id IS NULL THEN NULL
		      ELSE drift_task_created_at END,
		    drift_dm_settled_at=CASE
		      WHEN NOT $3 OR NOT drift_detected OR drift_episode_id IS NULL THEN NULL
		      ELSE drift_dm_settled_at END,
		    drift_alert_retry_at=CASE
		      WHEN NOT $3 THEN NULL
		      WHEN NOT drift_detected OR drift_episode_id IS NULL THEN now()
		      ELSE drift_alert_retry_at END,
		    drift_alert_retry_count=CASE
		      WHEN NOT $3 OR NOT drift_detected OR drift_episode_id IS NULL THEN 0
		      ELSE drift_alert_retry_count END,
		    drift_approval_id=CASE
		      WHEN NOT $3 THEN ''
		      WHEN NOT drift_detected OR drift_episode_id IS NULL THEN 'drift-alert-' || $7::text
		      ELSE drift_approval_id END
		WHERE id=$1
		RETURNING id,business_id,platform,external_id,drift_fields,
		          COALESCE(drift_episode_id, '00000000-0000-0000-0000-000000000000'::uuid),
		          drift_task_created_at,drift_dm_settled_at,drift_alert_retry_at,drift_alert_retry_count`
	var out domain.DriftAlertEpisode
	err = r.pool.QueryRow(ctx, q, id, raw, len(driftFields) > 0, driftFields, checkedAt, nextCheckAt, candidate).Scan(
		&out.SyncStateID, &out.BusinessID, &out.Platform, &out.ExternalID, &out.Fields, &out.EpisodeID,
		&out.TaskCreatedAt, &out.DMSettledAt, &out.RetryAt, &out.RetryCount)
	if err != nil {
		return out, fmt.Errorf("mark checked episode: %w", err)
	}
	return out, nil
}

func (r *syncStateRepository) ListPendingDriftAlerts(ctx context.Context) ([]domain.DriftAlertEpisode, error) {
	const q = `SELECT s.id,s.business_id,s.platform,s.external_id,s.drift_fields,s.drift_episode_id,
		s.drift_task_created_at,s.drift_dm_settled_at,s.drift_alert_retry_at,s.drift_alert_retry_count
		FROM sync_state s JOIN integrations i ON i.business_id=s.business_id
		 AND i.platform=s.platform AND i.external_id=s.external_id
		 AND i.status='active' AND i.deleted_at IS NULL
		WHERE s.drift_detected AND s.drift_episode_id IS NOT NULL
		  AND s.platform IN ('vk', 'yandex_business')
		  AND (s.drift_task_created_at IS NULL OR s.drift_dm_settled_at IS NULL)
		  AND COALESCE(s.drift_alert_retry_at, '-infinity'::timestamptz) <= now()
		ORDER BY s.drift_alert_retry_at NULLS FIRST, s.updated_at LIMIT 100`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list pending drift alerts: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.DriftAlertEpisode, error) {
		var x domain.DriftAlertEpisode
		err := row.Scan(&x.SyncStateID, &x.BusinessID, &x.Platform, &x.ExternalID, &x.Fields,
			&x.EpisodeID, &x.TaskCreatedAt, &x.DMSettledAt, &x.RetryAt, &x.RetryCount)
		return x, err
	})
}

// GetCurrentDriftAlert revalidates the exact episode against a live business
// and its exact active integration immediately before any notification side
// effect.
func (r *syncStateRepository) GetCurrentDriftAlert(ctx context.Context, id, episodeID uuid.UUID) (domain.DriftAlertEpisode, error) {
	const q = `SELECT s.id,s.business_id,s.platform,s.external_id,s.drift_fields,s.drift_episode_id,
		s.drift_task_created_at,s.drift_dm_settled_at,s.drift_alert_retry_at,s.drift_alert_retry_count
		FROM sync_state s
		JOIN businesses b ON b.id=s.business_id AND b.deleted_at IS NULL
		JOIN integrations i ON i.business_id=s.business_id
		 AND i.platform=s.platform AND i.external_id=s.external_id
		 AND i.status='active' AND i.deleted_at IS NULL
		WHERE s.id=$1 AND s.drift_episode_id=$2 AND s.drift_detected`
	var x domain.DriftAlertEpisode
	err := r.pool.QueryRow(ctx, q, id, episodeID).Scan(
		&x.SyncStateID, &x.BusinessID, &x.Platform, &x.ExternalID, &x.Fields,
		&x.EpisodeID, &x.TaskCreatedAt, &x.DMSettledAt, &x.RetryAt, &x.RetryCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return x, domain.ErrDriftEpisodeNotFound
	}
	if err != nil {
		return x, fmt.Errorf("get current drift alert: %w", err)
	}
	return x, nil
}

func (r *syncStateRepository) MarkDriftTaskCreated(ctx context.Context, id, episode uuid.UUID) error {
	return r.markEpisodeTime(ctx, id, episode, true)
}

func (r *syncStateRepository) MarkDriftDMSettled(ctx context.Context, id, episode uuid.UUID) error {
	return r.markEpisodeTime(ctx, id, episode, false)
}

func (r *syncStateRepository) ScheduleDriftAlertRetry(ctx context.Context, id, episode uuid.UUID, retryAt time.Time) error {
	const q = `UPDATE sync_state
		SET drift_alert_retry_at=$3,drift_alert_retry_count=drift_alert_retry_count+1,updated_at=now()
		WHERE id=$1 AND drift_episode_id=$2`
	tag, err := r.pool.Exec(ctx, q, id, episode, retryAt)
	if err != nil {
		return fmt.Errorf("schedule drift alert retry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrDriftEpisodeNotFound
	}
	return nil
}

func (r *syncStateRepository) markEpisodeTime(ctx context.Context, id, episode uuid.UUID, task bool) error {
	q := `UPDATE sync_state SET drift_dm_settled_at=now(),updated_at=now()
		WHERE id=$1 AND drift_episode_id=$2`
	if task {
		q = `UPDATE sync_state SET drift_task_created_at=now(),updated_at=now()
			WHERE id=$1 AND drift_episode_id=$2`
	}
	tag, err := r.pool.Exec(ctx, q, id, episode)
	if err != nil {
		return fmt.Errorf("mark drift alert state: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("drift episode changed")
	}
	return nil
}
