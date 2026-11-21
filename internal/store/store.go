package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound   = errors.New("registro nao encontrado")
	ErrValidation = errors.New("dados invalidos")
	ErrConflict   = errors.New("conflito de registro")
	ErrForbidden  = errors.New("operacao nao permitida")
)

type Store struct {
	mu sync.RWMutex
	db *sql.DB
}

func New(conn *sql.DB) *Store {
	return &Store{db: conn}
}

func (s *Store) DB() *sql.DB {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db
}

func (s *Store) ReplaceDB(conn *sql.DB) {
	s.mu.Lock()
	old := s.db
	s.db = conn
	s.mu.Unlock()
	if old != nil && old != conn {
		_ = old.Close()
	}
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func intToBool(v int) bool {
	return v != 0
}

func nowTimestamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func parseTimestamp(raw string) (time.Time, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func upsertSettingExec(ctx context.Context, exec execer, key, value string) error {
	_, err := exec.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = datetime('now')
	`, key, value)
	return err
}

func touchMutationExec(ctx context.Context, exec execer) error {
	return upsertSettingExec(ctx, exec, "last_data_mutation_at", nowTimestamp())
}

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func nullIntPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

func nullInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}
