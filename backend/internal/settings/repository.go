package settings

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/postgresutil"
	_ "github.com/lib/pq"
)

var (
	ErrDeploymentProfileInitialized = errors.New("setup is already completed; create a separate instance for a different business model")
	ErrUnsupportedDeploymentProfile = errors.New("unsupported deployment profile")
)

const deploymentProfileAdvisoryLock int64 = 0x6173746572736574

type Repository interface {
	GetAll(ctx context.Context) (map[string]string, error)
	SetMultiple(ctx context.Context, values map[string]string) error
	ConsumeInvitationCode(ctx context.Context, code string) error
	RestoreInvitationCode(ctx context.Context, code string) error
	InitializeDeploymentProfile(ctx context.Context, profile string) error
	Health(ctx context.Context) error
	Close() error
}

func NewRepository(ctx context.Context, databaseURL string) (Repository, string, error) {
	if databaseURL == "" {
		return NewMemoryRepository(), "memory", nil
	}
	repo, err := NewPostgresRepository(ctx, databaseURL)
	if err != nil {
		return nil, "", err
	}
	return repo, "postgres", nil
}

type MemoryRepository struct {
	mu      sync.RWMutex
	entries map[string]Entry
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{entries: map[string]Entry{}}
}

func (r *MemoryRepository) GetAll(context.Context) (map[string]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]string, len(r.entries))
	for key, entry := range r.entries {
		out[key] = entry.Value
	}
	return out, nil
}

func (r *MemoryRepository) SetMultiple(_ context.Context, values map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	for key, value := range values {
		r.entries[key] = Entry{Key: key, Value: value, UpdatedAt: now}
	}
	return nil
}

func (r *MemoryRepository) ConsumeInvitationCode(_ context.Context, code string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	next, err := consumeInvitationCodeValue(r.entries[KeyInvitationCodes].Value, code)
	if err != nil {
		return err
	}
	r.entries[KeyInvitationCodes] = Entry{Key: KeyInvitationCodes, Value: next, UpdatedAt: time.Now().UTC()}
	return nil
}

func (r *MemoryRepository) RestoreInvitationCode(_ context.Context, code string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	next, err := restoreInvitationCodeValue(r.entries[KeyInvitationCodes].Value, code)
	if err != nil {
		return err
	}
	r.entries[KeyInvitationCodes] = Entry{Key: KeyInvitationCodes, Value: next, UpdatedAt: time.Now().UTC()}
	return nil
}

func (r *MemoryRepository) InitializeDeploymentProfile(_ context.Context, profile string) error {
	profile = strings.TrimSpace(profile)
	if !isProfile(profile) {
		return fmt.Errorf("%w %q", ErrUnsupportedDeploymentProfile, profile)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	current := make(map[string]string, 3)
	for _, key := range []string{KeySetupCompleted, KeyEnabledProfiles, KeyDefaultProfile} {
		current[key] = r.entries[key].Value
	}
	if deploymentProfileInitialized(current) {
		return ErrDeploymentProfileInitialized
	}
	now := time.Now().UTC()
	for key, value := range deploymentProfileValues(profile) {
		r.entries[key] = Entry{Key: key, Value: value, UpdatedAt: now}
	}
	return nil
}

func (r *MemoryRepository) Health(context.Context) error {
	return nil
}

func (r *MemoryRepository) Close() error {
	return nil
}

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(ctx context.Context, databaseURL string) (*PostgresRepository, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	repo := &PostgresRepository{db: db}
	if err := repo.Health(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := repo.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return repo, nil
}

func (r *PostgresRepository) migrate(ctx context.Context) error {
	return postgresutil.WithSchemaMigrationLock(ctx, r.db, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS settings (
  key VARCHAR(100) PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
`)
		return err
	})
}

func (r *PostgresRepository) GetAll(ctx context.Context) (map[string]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SetMultiple(ctx context.Context, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if err = setMultipleTx(ctx, tx, values); err != nil {
		return err
	}
	err = tx.Commit()
	return err
}

func (r *PostgresRepository) ConsumeInvitationCode(ctx context.Context, code string) error {
	return r.updateInvitationCodes(ctx, code, consumeInvitationCodeValue)
}

func (r *PostgresRepository) RestoreInvitationCode(ctx context.Context, code string) error {
	return r.updateInvitationCodes(ctx, code, restoreInvitationCodeValue)
}

func (r *PostgresRepository) updateInvitationCodes(ctx context.Context, code string, update func(string, string) (string, error)) (err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var current string
	err = tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=$1 FOR UPDATE`, KeyInvitationCodes).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		current = "[]"
		err = nil
	} else if err != nil {
		return err
	}
	next, err := update(current, code)
	if err != nil {
		return err
	}
	if err = setMultipleTx(ctx, tx, map[string]string{KeyInvitationCodes: next}); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PostgresRepository) InitializeDeploymentProfile(ctx context.Context, profile string) (err error) {
	profile = strings.TrimSpace(profile)
	if !isProfile(profile) {
		return fmt.Errorf("%w %q", ErrUnsupportedDeploymentProfile, profile)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, deploymentProfileAdvisoryLock); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
SELECT key, value
FROM settings
WHERE key IN ($1, $2, $3)
`, KeySetupCompleted, KeyEnabledProfiles, KeyDefaultProfile)
	if err != nil {
		return err
	}
	current := map[string]string{}
	for rows.Next() {
		var key, value string
		if err = rows.Scan(&key, &value); err != nil {
			_ = rows.Close()
			return err
		}
		current[key] = value
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	if deploymentProfileInitialized(current) {
		return ErrDeploymentProfileInitialized
	}
	if err = setMultipleTx(ctx, tx, deploymentProfileValues(profile)); err != nil {
		return err
	}
	return tx.Commit()
}

func setMultipleTx(ctx context.Context, tx *sql.Tx, values map[string]string) error {
	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO settings(key, value, updated_at)
VALUES($1, $2, now())
ON CONFLICT(key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for key, value := range values {
		if _, err := stmt.ExecContext(ctx, key, value); err != nil {
			return err
		}
	}
	return nil
}

func consumeInvitationCodeValue(raw, code string) (string, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", ErrInvitationCodeRequired
	}
	codes := parseStringList(raw, []string{})
	for index, candidate := range codes {
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) != 1 {
			continue
		}
		remaining := append([]string{}, codes[:index]...)
		remaining = append(remaining, codes[index+1:]...)
		encoded, err := json.Marshal(remaining)
		return string(encoded), err
	}
	return "", ErrInvitationCodeInvalid
}

func restoreInvitationCodeValue(raw, code string) (string, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", ErrInvitationCodeRequired
	}
	codes := parseStringList(raw, []string{})
	for _, candidate := range codes {
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) == 1 {
			encoded, err := json.Marshal(codes)
			return string(encoded), err
		}
	}
	encoded, err := json.Marshal(append(codes, code))
	return string(encoded), err
}

func deploymentProfileInitialized(values map[string]string) bool {
	return parseBool(values[KeySetupCompleted]) || values[KeyEnabledProfiles] != "" || values[KeyDefaultProfile] != ""
}

func deploymentProfileValues(profile string) map[string]string {
	encodedProfiles, _ := json.Marshal([]string{profile})
	return map[string]string{
		KeyDefaultProfile:  profile,
		KeyEnabledProfiles: string(encodedProfiles),
		KeySetupCompleted:  "true",
	}
}

func (r *PostgresRepository) Health(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

func (r *PostgresRepository) Close() error {
	return r.db.Close()
}
