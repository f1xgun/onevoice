package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/f1xgun/onevoice/pkg/crypto"
	"github.com/f1xgun/onevoice/pkg/domain"
)

var (
	rowsProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "rekey_rows_processed_total",
		Help: "Count of rows processed by cmd/rekey, labelled by result.",
	}, []string{"result"})

	batchDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "rekey_batch_duration_seconds",
		Help:    "Wall-clock seconds per rekey batch tx.",
		Buckets: prometheus.DefBuckets,
	})

	remainingGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "rekey_remaining_rows",
		Help: "Rows still needing rekey (sampled every 5s while running).",
	})
)

// txBeginner is the subset of *pgxpool.Pool used by processBatch, extracted
// so the Rekeyer can be exercised in unit tests without a live Postgres pool.
type txBeginner interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

// Rekeyer drives the rekey loop for all integrations rows below targetVersion.
type Rekeyer struct {
	repo          domain.IntegrationRepository
	envelope      *crypto.Envelope
	legacy        *crypto.Encryptor
	pool          txBeginner
	targetVersion int16
	batch         int
	concurrency   int
	dryRun        bool
	log           *slog.Logger
}

// NewRekeyer constructs a Rekeyer. legacy may be nil when no legacy flat-AES
// rows are expected; the run will error if a nil-wrapped_dek row is encountered
// without a legacy Encryptor available. pool accepts any txBeginner; callers
// pass *pgxpool.Pool which satisfies the interface.
func NewRekeyer(
	repo domain.IntegrationRepository,
	envelope *crypto.Envelope,
	legacy *crypto.Encryptor,
	pool txBeginner,
	targetVersion int16,
	batch, concurrency int,
	dryRun bool,
	log *slog.Logger,
) *Rekeyer {
	return &Rekeyer{
		repo:          repo,
		envelope:      envelope,
		legacy:        legacy,
		pool:          pool,
		targetVersion: targetVersion,
		batch:         batch,
		concurrency:   concurrency,
		dryRun:        dryRun,
		log:           log,
	}
}

// Run executes the rekey loop. In dry-run mode it reports the count of rows
// needing rekey without performing any writes.
func (r *Rekeyer) Run(ctx context.Context) error {
	if r.dryRun {
		n, err := r.repo.CountRekeyRemaining(ctx, r.targetVersion)
		if err != nil {
			return fmt.Errorf("rekey: dry-run count: %w", err)
		}
		r.log.Info("dry-run: rows requiring rekey", "count", n, "target_version", r.targetVersion)
		return nil
	}

	remainingTicker := time.NewTicker(5 * time.Second)
	defer remainingTicker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-remainingTicker.C:
				if n, err := r.repo.CountRekeyRemaining(ctx, r.targetVersion); err == nil {
					remainingGauge.Set(float64(n))
				}
			}
		}
	}()

	var wg sync.WaitGroup
	errCh := make(chan error, r.concurrency)
	for w := 0; w < r.concurrency; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			if err := r.workerLoop(ctx, workerID); err != nil {
				errCh <- err
			}
		}(w)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Rekeyer) workerLoop(ctx context.Context, workerID int) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		start := time.Now()
		processed, err := r.processBatch(ctx)
		batchDuration.Observe(time.Since(start).Seconds())
		if err != nil {
			rowsProcessed.WithLabelValues("error").Inc()
			r.log.ErrorContext(ctx, "rekey batch failed", "worker", workerID, "error", err)
			return err
		}
		if processed == 0 {
			return nil
		}
		r.log.InfoContext(ctx, "rekey batch", "worker", workerID, "rows", processed, "elapsed_sec", time.Since(start).Seconds())
	}
}

func (r *Rekeyer) processBatch(ctx context.Context) (int, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("rekey: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := r.repo.SelectForRekey(ctx, tx, r.targetVersion, r.batch)
	if err != nil {
		return 0, fmt.Errorf("rekey: select: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}

	processed := 0
	for _, row := range rows {
		if err := r.rekeyRow(ctx, tx, row); err != nil {
			rowsProcessed.WithLabelValues("error").Inc()
			return processed, err
		}
		rowsProcessed.WithLabelValues("success").Inc()
		processed++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("rekey: commit: %w", err)
	}
	return processed, nil
}

func (r *Rekeyer) rekeyRow(ctx context.Context, tx pgx.Tx, row domain.Integration) error {
	var plaintexts [][]byte

	if row.WrappedDEK == nil {
		if r.legacy == nil {
			return fmt.Errorf("rekey: legacy rows present but ENCRYPTION_KEY not configured (row id=%s)", row.ID)
		}
		pts := make([][]byte, 3)
		var err error
		if len(row.EncryptedAccessToken) > 0 {
			pts[0], err = r.legacy.Decrypt(row.EncryptedAccessToken)
			if err != nil {
				return fmt.Errorf("rekey: legacy decrypt access id=%s: %w", row.ID, err)
			}
		}
		if len(row.EncryptedRefreshToken) > 0 {
			pts[1], err = r.legacy.Decrypt(row.EncryptedRefreshToken)
			if err != nil {
				return fmt.Errorf("rekey: legacy decrypt refresh id=%s: %w", row.ID, err)
			}
		}
		if len(row.EncryptedUserToken) > 0 {
			pts[2], err = r.legacy.Decrypt(row.EncryptedUserToken)
			if err != nil {
				return fmt.Errorf("rekey: legacy decrypt user id=%s: %w", row.ID, err)
			}
		}
		plaintexts = pts
	} else {
		ciphertexts := [][]byte{row.EncryptedAccessToken, row.EncryptedRefreshToken, row.EncryptedUserToken}
		pts, _, err := r.envelope.DecryptForRow(ctx, row.ID, row.Platform, ciphertexts, row.WrappedDEK)
		if err != nil {
			return fmt.Errorf("rekey: envelope decrypt id=%s: %w", row.ID, err)
		}
		plaintexts = pts
	}

	// Defensive defer: wipes any slice that was not already eagerly zeroed
	// (e.g. on an early-return error path below).
	for i := range plaintexts {
		i := i
		defer crypto.Wipe(plaintexts[i])
	}

	ciphertexts, wrappedDEK, keyVersion, fingerprint, err := r.envelope.EncryptForRow(ctx, row.ID, row.Platform, plaintexts)
	if err != nil {
		return fmt.Errorf("rekey: re-encrypt id=%s: %w", row.ID, err)
	}
	for i := range plaintexts {
		crypto.Wipe(plaintexts[i])
	}

	updated := domain.Integration{
		ID:                       row.ID,
		EncryptedAccessToken:     ciphertexts[0],
		EncryptedRefreshToken:    ciphertexts[1],
		EncryptedUserToken:       ciphertexts[2],
		WrappedDEK:               wrappedDEK,
		KeyVersion:               keyVersion,
		EncryptionKeyFingerprint: fingerprint,
	}
	if err := r.repo.UpdateEnvelopeFieldsTx(ctx, tx, updated); err != nil {
		return fmt.Errorf("rekey: update id=%s: %w", row.ID, err)
	}
	return nil
}
