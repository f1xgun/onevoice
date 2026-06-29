package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/crypto"
	"github.com/f1xgun/onevoice/pkg/crypto/kmsfake"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// fakeRepo is an in-memory IntegrationRepository for unit tests.
type fakeRepo struct {
	rows       []domain.Integration
	updateFunc func(integ domain.Integration) error
	countFunc  func() (int, error)
}

func (f *fakeRepo) SelectForRekey(_ context.Context, _ pgx.Tx, targetVersion int16, limit int) ([]domain.Integration, error) {
	var out []domain.Integration
	for _, r := range f.rows {
		if r.WrappedDEK == nil || r.KeyVersion < targetVersion {
			out = append(out, r)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (f *fakeRepo) UpdateEnvelopeFieldsTx(_ context.Context, _ pgx.Tx, integ domain.Integration) error {
	if f.updateFunc != nil {
		return f.updateFunc(integ)
	}
	for i, r := range f.rows {
		if r.ID != integ.ID {
			continue
		}
		f.rows[i].EncryptedAccessToken = integ.EncryptedAccessToken
		f.rows[i].EncryptedRefreshToken = integ.EncryptedRefreshToken
		f.rows[i].EncryptedUserToken = integ.EncryptedUserToken
		f.rows[i].WrappedDEK = integ.WrappedDEK
		f.rows[i].KeyVersion = integ.KeyVersion
		f.rows[i].EncryptionKeyFingerprint = integ.EncryptionKeyFingerprint
		return nil
	}
	return errors.New("fakeRepo: row not found")
}

func (f *fakeRepo) CountRekeyRemaining(_ context.Context, targetVersion int16) (int, error) {
	if f.countFunc != nil {
		return f.countFunc()
	}
	n := 0
	for _, r := range f.rows {
		if r.WrappedDEK == nil || r.KeyVersion < targetVersion {
			n++
		}
	}
	return n, nil
}

func (f *fakeRepo) Create(_ context.Context, _ *domain.Integration) error { return nil }
func (f *fakeRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Integration, error) {
	return nil, domain.ErrIntegrationNotFound
}
func (f *fakeRepo) GetByBusinessAndPlatform(_ context.Context, _ uuid.UUID, _ string) (*domain.Integration, error) {
	return nil, domain.ErrIntegrationNotFound
}
func (f *fakeRepo) ListByBusinessID(_ context.Context, _ uuid.UUID) ([]domain.Integration, error) {
	return nil, nil
}
func (f *fakeRepo) ListByBusinessAndPlatform(_ context.Context, _ uuid.UUID, _ string) ([]domain.Integration, error) {
	return nil, nil
}
func (f *fakeRepo) GetByBusinessPlatformExternal(_ context.Context, _ uuid.UUID, _, _ string) (*domain.Integration, error) {
	return nil, domain.ErrIntegrationNotFound
}
func (f *fakeRepo) GetActiveByPlatformExternal(_ context.Context, _, _ string) (*domain.Integration, error) {
	return nil, domain.ErrIntegrationNotFound
}
func (f *fakeRepo) ListAllActiveByPlatforms(_ context.Context, _ []string) ([]domain.Integration, error) {
	return nil, nil
}
func (f *fakeRepo) Update(_ context.Context, _ *domain.Integration) error         { return nil }
func (f *fakeRepo) Delete(_ context.Context, _ uuid.UUID) error                   { return nil }
func (f *fakeRepo) SoftDelete(_ context.Context, _ uuid.UUID) error               { return nil }
func (f *fakeRepo) DeleteOlderThan(_ context.Context, _ time.Time) (int64, error) { return 0, nil }
func (f *fakeRepo) MarkTokenExpired(_ context.Context, _ uuid.UUID, _, _ string) (int64, error) {
	return 0, nil
}
func (f *fakeRepo) CountIntegrationsWithDifferentFingerprint(_ context.Context, _ string) (int, error) {
	return 0, nil
}

// fakeTxPool satisfies the *pgxpool.Pool interface only enough for processBatch.
// We pass nil — processBatch calls BeginTx which needs a real pool — so unit
// tests call rekeyRow directly instead of processBatch.

func testEnvelope(t *testing.T) *crypto.Envelope {
	t.Helper()
	fake := kmsfake.New()
	return crypto.NewEnvelope(fake, nil, "test-key-id", map[string]int16{"1": 1})
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError + 100}))
}

func TestRekeyDryRunCount(t *testing.T) {
	env := testEnvelope(t)
	legacyRow := domain.Integration{ID: uuid.New(), Platform: "telegram"}
	envelopeRow := domain.Integration{ID: uuid.New(), Platform: "telegram", KeyVersion: 1, WrappedDEK: []byte("x")}
	repo := &fakeRepo{
		rows: []domain.Integration{legacyRow, envelopeRow},
	}
	r := NewRekeyer(repo, env, nil, nil, 2, 100, 1, true, discardLogger())
	err := r.Run(context.Background())
	require.NoError(t, err)
	n, _ := repo.CountRekeyRemaining(context.Background(), 2)
	assert.Equal(t, 2, n)
	assert.Nil(t, repo.rows[0].WrappedDEK, "dry-run must not update legacy row")
	assert.Equal(t, []byte("x"), repo.rows[1].WrappedDEK, "dry-run must not overwrite envelope row")
}

func TestRekeyRow_LegacyPath(t *testing.T) {
	legacyKey := make([]byte, 32)
	for i := range legacyKey {
		legacyKey[i] = byte(i + 1)
	}
	enc, err := crypto.NewEncryptor(legacyKey)
	require.NoError(t, err)

	plainAccess := []byte("access-token-value")
	ct, err := enc.Encrypt(plainAccess)
	require.NoError(t, err)

	id := uuid.New()
	row := domain.Integration{
		ID:                   id,
		Platform:             "telegram",
		EncryptedAccessToken: ct,
	}

	env := testEnvelope(t)
	repo := &fakeRepo{rows: []domain.Integration{row}}
	r := NewRekeyer(repo, env, enc, nil, 1, 100, 1, false, discardLogger())

	err = r.rekeyRow(context.Background(), nil, row)
	require.NoError(t, err)
}

func TestRekeyRow_EnvelopePath(t *testing.T) {
	kFake := kmsfake.New()
	envV1 := crypto.NewEnvelope(kFake, nil, "key-id", map[string]int16{"1": 1})

	id := uuid.New()
	plaintexts := [][]byte{[]byte("access"), []byte("refresh"), nil}
	cts, wrapped, _, _, err := envV1.EncryptForRow(context.Background(), id, "vk", plaintexts)
	require.NoError(t, err)

	row := domain.Integration{
		ID:                    id,
		Platform:              "vk",
		EncryptedAccessToken:  cts[0],
		EncryptedRefreshToken: cts[1],
		EncryptedUserToken:    cts[2],
		WrappedDEK:            wrapped,
		KeyVersion:            1,
	}

	kFake.RotateToVersion(2)
	envV2 := crypto.NewEnvelope(kFake, nil, "key-id", map[string]int16{"1": 1, "2": 2})
	repo := &fakeRepo{rows: []domain.Integration{row}}
	r := NewRekeyer(repo, envV2, nil, nil, 2, 100, 1, false, discardLogger())

	err = r.rekeyRow(context.Background(), nil, row)
	require.NoError(t, err)
}

func TestRekeyRow_LegacyWithoutLegacyKey(t *testing.T) {
	id := uuid.New()
	row := domain.Integration{
		ID:                   id,
		Platform:             "telegram",
		EncryptedAccessToken: []byte("junk"),
	}
	env := testEnvelope(t)
	repo := &fakeRepo{rows: []domain.Integration{row}}
	r := NewRekeyer(repo, env, nil, nil, 1, 100, 1, false, discardLogger())

	err := r.rekeyRow(context.Background(), nil, row)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ENCRYPTION_KEY not configured")
}

func TestRekeyRow_UpdateFailure(t *testing.T) {
	kFake := kmsfake.New()
	envV1 := crypto.NewEnvelope(kFake, nil, "key-id", map[string]int16{"1": 1})

	id := uuid.New()
	plaintexts := [][]byte{[]byte("access"), nil, nil}
	cts, wrapped, _, _, err := envV1.EncryptForRow(context.Background(), id, "telegram", plaintexts)
	require.NoError(t, err)

	row := domain.Integration{
		ID:                   id,
		Platform:             "telegram",
		EncryptedAccessToken: cts[0],
		WrappedDEK:           wrapped,
		KeyVersion:           1,
	}

	kFake.RotateToVersion(2)
	envV2 := crypto.NewEnvelope(kFake, nil, "key-id", map[string]int16{"1": 1, "2": 2})
	repo := &fakeRepo{
		rows: []domain.Integration{row},
		updateFunc: func(_ domain.Integration) error {
			return errors.New("db write error")
		},
	}
	r := NewRekeyer(repo, envV2, nil, nil, 2, 100, 1, false, discardLogger())

	err = r.rekeyRow(context.Background(), nil, row)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db write error")
}

func TestRekeyRow_UnmappedVersionAborts(t *testing.T) {
	kFake := kmsfake.New()
	envV1 := crypto.NewEnvelope(kFake, nil, "key-id", map[string]int16{"1": 1})

	row := newV1Row(t, envV1)

	kFake.RotateToVersion(2)
	envUnmapped := crypto.NewEnvelope(kFake, nil, "key-id", map[string]int16{"1": 1})

	repo := &fakeRepo{rows: []domain.Integration{row}}
	r := NewRekeyer(repo, envUnmapped, nil, nil, 2, 100, 1, false, discardLogger())

	err := r.rekeyRow(context.Background(), nil, row)
	require.Error(t, err, "rekeyRow must abort when KMS version resolves below target")
	assert.ErrorIs(t, err, crypto.ErrUnmappedKMSVersion)

	assert.Equal(t, int16(1), repo.rows[0].KeyVersion,
		"row must not be written with key_version below target (would re-select forever)")

	n, err := repo.CountRekeyRemaining(context.Background(), 2)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "unmapped row would be re-selected on the next batch")
}

func TestRekeyRunAbortsOnUnmappedVersion(t *testing.T) {
	kFake := kmsfake.New()
	envV1 := crypto.NewEnvelope(kFake, nil, "key-id", map[string]int16{"1": 1})

	row := newV1Row(t, envV1)

	kFake.RotateToVersion(2)
	envUnmapped := crypto.NewEnvelope(kFake, nil, "key-id", map[string]int16{"1": 1})

	updateCount := 0
	repo := &fakeRepo{
		rows: []domain.Integration{row},
		updateFunc: func(_ domain.Integration) error {
			updateCount++
			return nil
		},
	}
	r := NewRekeyer(repo, envUnmapped, nil, fakePool{}, 2, 100, 1, false, discardLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	select {
	case err := <-done:
		require.Error(t, err, "Run must abort instead of looping when KMS version is unmapped")
		assert.ErrorIs(t, err, crypto.ErrUnmappedKMSVersion)
		assert.Equal(t, 0, updateCount, "no row may be written below target version")
	case <-ctx.Done():
		t.Fatal("Run did not return: without the no-progress guard it re-selects key_version=0 rows forever")
	}
}

// noopTx is a pgx.Tx stub whose Commit and Rollback are no-ops. All other
// methods are not exercised by processBatch so they panic to catch misuse.
type noopTx struct{}

func (noopTx) Begin(_ context.Context) (pgx.Tx, error) { return noopTx{}, nil }
func (noopTx) Commit(_ context.Context) error          { return nil }
func (noopTx) Rollback(_ context.Context) error        { return nil }
func (noopTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	panic("noopTx: CopyFrom not implemented")
}
func (noopTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
	panic("noopTx: SendBatch not implemented")
}
func (noopTx) LargeObjects() pgx.LargeObjects { panic("noopTx: LargeObjects not implemented") }
func (noopTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	panic("noopTx: Prepare not implemented")
}
func (noopTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	panic("noopTx: Exec not implemented")
}
func (noopTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	panic("noopTx: Query not implemented")
}
func (noopTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	panic("noopTx: QueryRow not implemented")
}
func (noopTx) Conn() *pgx.Conn { return nil }

// fakePool is a txBeginner that always returns a noopTx.
type fakePool struct{}

func (fakePool) BeginTx(_ context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	return noopTx{}, nil
}

// newV1Row encrypts a plaintext access token at v1 and returns the Integration.
func newV1Row(t *testing.T, env *crypto.Envelope) domain.Integration {
	t.Helper()
	id := uuid.New()
	pts := [][]byte{[]byte("access-token"), nil, nil}
	cts, wrapped, _, _, err := env.EncryptForRow(context.Background(), id, "telegram", pts)
	require.NoError(t, err)
	return domain.Integration{
		ID:                   id,
		Platform:             "telegram",
		EncryptedAccessToken: cts[0],
		WrappedDEK:           wrapped,
		KeyVersion:           1,
	}
}

// newLegacyRow encrypts a plaintext access token with the legacy AES encryptor.
func newLegacyRow(t *testing.T, enc *crypto.Encryptor) domain.Integration {
	t.Helper()
	ct, err := enc.Encrypt([]byte("legacy-access-token"))
	require.NoError(t, err)
	return domain.Integration{
		ID:                   uuid.New(),
		Platform:             "telegram",
		EncryptedAccessToken: ct,
	}
}

func TestRekeyIdempotent(t *testing.T) {
	legacyKey := make([]byte, 32)
	for i := range legacyKey {
		legacyKey[i] = byte(i + 1)
	}
	enc, err := crypto.NewEncryptor(legacyKey)
	require.NoError(t, err)

	kFake := kmsfake.New()
	envV1 := crypto.NewEnvelope(kFake, enc, "key-id", map[string]int16{"1": 1})

	legacy1 := newLegacyRow(t, enc)
	legacy2 := newLegacyRow(t, enc)
	v1row1 := newV1Row(t, envV1)
	v1row2 := newV1Row(t, envV1)

	kFake.RotateToVersion(2)
	envV2 := crypto.NewEnvelope(kFake, enc, "key-id", map[string]int16{"1": 1, "2": 2})

	alreadyV2id := uuid.New()
	pts := [][]byte{[]byte("already-v2"), nil, nil}
	v2cts, v2wrapped, _, _, err := envV2.EncryptForRow(context.Background(), alreadyV2id, "telegram", pts)
	require.NoError(t, err)
	v2row := domain.Integration{
		ID:                   alreadyV2id,
		Platform:             "telegram",
		EncryptedAccessToken: v2cts[0],
		WrappedDEK:           v2wrapped,
		KeyVersion:           2,
	}

	repo := &fakeRepo{
		rows: []domain.Integration{legacy1, legacy2, v1row1, v1row2, v2row},
	}

	r := NewRekeyer(repo, envV2, enc, fakePool{}, 2, 100, 1, false, discardLogger())

	err = r.Run(context.Background())
	require.NoError(t, err)

	for i, row := range repo.rows {
		assert.NotNil(t, row.WrappedDEK, "row %d must have WrappedDEK after rekey", i)
		assert.Equal(t, int16(2), row.KeyVersion, "row %d must be at v2 after rekey", i)
	}

	n, err := repo.CountRekeyRemaining(context.Background(), 2)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	updateCount := 0
	repo.updateFunc = func(integ domain.Integration) error {
		updateCount++
		for i, row := range repo.rows {
			if row.ID == integ.ID {
				repo.rows[i] = integ
				return nil
			}
		}
		return nil
	}

	err = r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, updateCount, "second Run must issue zero updates")
}

func TestRekeyExitsOnFailure(t *testing.T) {
	kFake := kmsfake.New()
	envV1 := crypto.NewEnvelope(kFake, nil, "key-id", map[string]int16{"1": 1})

	id := uuid.New()
	pts := [][]byte{[]byte("access"), nil, nil}
	cts, wrapped, _, _, err := envV1.EncryptForRow(context.Background(), id, "telegram", pts)
	require.NoError(t, err)

	row := domain.Integration{
		ID:                   id,
		Platform:             "telegram",
		EncryptedAccessToken: cts[0],
		WrappedDEK:           wrapped,
		KeyVersion:           1,
	}

	kFake.RotateToVersion(2)
	envV2 := crypto.NewEnvelope(kFake, nil, "key-id", map[string]int16{"1": 1, "2": 2})

	repo := &fakeRepo{
		rows: []domain.Integration{row},
		updateFunc: func(_ domain.Integration) error {
			return errors.New("simulated db failure")
		},
	}

	r := NewRekeyer(repo, envV2, nil, fakePool{}, 2, 100, 1, false, discardLogger())
	err = r.Run(context.Background())
	require.Error(t, err, "Run must return non-nil error when a row update fails")
	assert.Contains(t, err.Error(), "simulated db failure")
}
