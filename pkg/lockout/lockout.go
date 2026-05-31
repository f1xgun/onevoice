package lockout

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Tier classifies the current lockout state of an (email, /16 IP) tuple.
type Tier int

const (
	// TierNormal — fewer than FailThresholdCaptcha failures recorded.
	// Login proceeds without friction.
	TierNormal Tier = iota
	// TierCaptcha — between FailThresholdCaptcha and FailThresholdLock-1
	// failures. Handler MUST enforce a SmartCaptcha challenge before
	// delegating to userService.Login.
	TierCaptcha
	// TierLocked — FailThresholdLock or more failures. Middleware
	// short-circuits with 423 Locked + retry_after_seconds.
	TierLocked
)

// Default thresholds + TTL. Overridable via Config or LOCKOUT_* env vars
// (see services/api/internal/config/config.go).
const (
	DefaultFailThresholdCaptcha = 4
	DefaultFailThresholdLock    = 10
	DefaultDuration             = 15 * time.Minute
	DefaultKeyPrefix            = "onevoice:lockout:"
	// scanPageSize bounds each SCAN page in ClearAllForEmail. Small enough
	// to keep p99 latency under a millisecond on a healthy Redis, large
	// enough to keep the iteration count low for the typical case (a
	// handful of /16 buckets per email_hash).
	scanPageSize = 100
)

// Config holds the tunables for a Lockout instance.
type Config struct {
	// FailThresholdCaptcha — counter at which TierCaptcha kicks in.
	// Defaults to DefaultFailThresholdCaptcha (4) when zero.
	FailThresholdCaptcha int
	// FailThresholdLock — counter at which TierLocked kicks in.
	// Defaults to DefaultFailThresholdLock (10) when zero.
	FailThresholdLock int
	// Duration — Redis TTL and lock window. Defaults to DefaultDuration
	// (15m) when zero.
	Duration time.Duration
	// KeyPrefix — Redis key prefix. Defaults to DefaultKeyPrefix when "".
	KeyPrefix string
}

// Lockout is the Redis-backed counter package. Construct via New. Safe
// for concurrent use across goroutines (Redis INCR is atomic).
type Lockout struct {
	rdb *redis.Client
	cfg Config
}

// New constructs a Lockout backed by rdb. Missing Config fields fall back
// to the Default* constants so callers can pass a zero-Config for the
// production defaults.
func New(rdb *redis.Client, cfg Config) *Lockout {
	if cfg.FailThresholdCaptcha == 0 {
		cfg.FailThresholdCaptcha = DefaultFailThresholdCaptcha
	}
	if cfg.FailThresholdLock == 0 {
		cfg.FailThresholdLock = DefaultFailThresholdLock
	}
	if cfg.Duration == 0 {
		cfg.Duration = DefaultDuration
	}
	if cfg.KeyPrefix == "" {
		cfg.KeyPrefix = DefaultKeyPrefix
	}
	return &Lockout{rdb: rdb, cfg: cfg}
}

// emailHash returns the lowercase-trimmed sha256 hex of email. The lowercase
// fold + trim are deliberate — case-insensitive emails must collapse to the
// same key, otherwise an attacker rotating Foo@/foo@/FOO@ casing would never
// trip the counter.
func emailHash(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:])
}

// keyFor returns the canonical Redis key for (email, /16-bucket IP). Always
// holds the form prefix:hash:net16 so SCAN-by-email_hash works in
// ClearAllForEmail.
func (l *Lockout) keyFor(email, ipNet16 string) string {
	return l.cfg.KeyPrefix + emailHash(email) + ":" + ipNet16
}

// RecordFailure increments the failure counter and returns the new count.
// On first-create the key is given a TTL of cfg.Duration so the lock
// auto-expires without operator action.
//
// The INCR + PTTL probe are batched into a single TxPipeline so a parallel
// EXPIRE racing with the first INCR cannot accidentally clear the TTL
// (the read of PTTL == -1 happens atomically with INCR's result).
func (l *Lockout) RecordFailure(ctx context.Context, email, ipNet16 string) (int, error) {
	key := l.keyFor(email, ipNet16)
	pipe := l.rdb.TxPipeline()
	incr := pipe.Incr(ctx, key)
	ttl := pipe.PTTL(ctx, key)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("lockout: incr: %w", err)
	}
	// PTTL == -1 means the key exists but has no TTL — happens only on the
	// first INCR. PTTL == -2 means the key vanished between INCR and PTTL,
	// which only happens under explicit DEL; treat the same way (set TTL).
	if ttl.Val() < 0 {
		if err := l.rdb.PExpire(ctx, key, l.cfg.Duration).Err(); err != nil {
			return 0, fmt.Errorf("lockout: expire: %w", err)
		}
	}
	return int(incr.Val()), nil
}

// GetTier returns the current Tier for the (email, /16 IP) tuple. A missing
// key is TierNormal (no prior failures). A Redis error returns TierNormal
// + err so the caller can decide fail-open vs fail-closed (the middleware
// fails open — a Redis outage must not lock every legitimate user out).
func (l *Lockout) GetTier(ctx context.Context, email, ipNet16 string) (Tier, error) {
	v, err := l.rdb.Get(ctx, l.keyFor(email, ipNet16)).Int()
	if errors.Is(err, redis.Nil) {
		return TierNormal, nil
	}
	if err != nil {
		return TierNormal, fmt.Errorf("lockout: get: %w", err)
	}
	switch {
	case v >= l.cfg.FailThresholdLock:
		return TierLocked, nil
	case v >= l.cfg.FailThresholdCaptcha:
		return TierCaptcha, nil
	default:
		return TierNormal, nil
	}
}

// IsLocked is a convenience wrapper: (GetTier == TierLocked).
func (l *Lockout) IsLocked(ctx context.Context, email, ipNet16 string) (bool, error) {
	t, err := l.GetTier(ctx, email, ipNet16)
	return t == TierLocked, err
}

// TTL returns the remaining lock duration. Zero when the key has no TTL or
// does not exist. Useful for populating the Retry-After header / retry_after_seconds
// JSON field on the 423 response.
func (l *Lockout) TTL(ctx context.Context, email, ipNet16 string) (time.Duration, error) {
	d, err := l.rdb.PTTL(ctx, l.keyFor(email, ipNet16)).Result()
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, nil
	}
	return d, nil
}

// Clear deletes the (email, /16 IP) key. Called on successful login so a
// fresh batch of failures doesn't accumulate stale state.
func (l *Lockout) Clear(ctx context.Context, email, ipNet16 string) error {
	return l.rdb.Del(ctx, l.keyFor(email, ipNet16)).Err()
}

// ClearAllForEmail enumerates every per-/16 variant of the email's lockout
// keys and deletes them. For password-reset self-unlock where the caller
// does not know which /16 buckets accumulated failures.
//
// SCAN is safe under load (non-blocking, cursor-based). The MATCH glob is
// anchored on prefix:hash: so it never iterates beyond this email's keys.
func (l *Lockout) ClearAllForEmail(ctx context.Context, email string) error {
	match := l.cfg.KeyPrefix + emailHash(email) + ":*"
	iter := l.rdb.Scan(ctx, 0, match, scanPageSize).Iterator()
	for iter.Next(ctx) {
		if err := l.rdb.Del(ctx, iter.Val()).Err(); err != nil {
			return fmt.Errorf("lockout: del during clear-all: %w", err)
		}
	}
	if err := iter.Err(); err != nil {
		return fmt.Errorf("lockout: scan during clear-all: %w", err)
	}
	return nil
}
