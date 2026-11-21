package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Open(dbPath string) (*sql.DB, error) {
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("criar diretorio do banco: %w", err)
		}
	}

	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("abrir sqlite: %w", err)
	}
	conn.SetMaxOpenConns(1)

	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	if _, err := conn.Exec("PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL; PRAGMA busy_timeout = 5000;"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("configurar pragmas sqlite: %w", err)
	}

	if err := applyMigrations(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}

	if err := seedDefaults(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}

	return conn, nil
}

func applyMigrations(conn *sql.DB) error {
	if _, err := conn.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT (datetime('now')))`); err != nil {
		return fmt.Errorf("criar tabela schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("listar migracoes: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var count int
		if err := conn.QueryRow("SELECT COUNT(1) FROM schema_migrations WHERE version = ?", name).Scan(&count); err != nil {
			return fmt.Errorf("verificar migracao %s: %w", name, err)
		}
		if count > 0 {
			continue
		}

		content, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("ler migracao %s: %w", name, err)
		}

		tx, err := conn.Begin()
		if err != nil {
			return fmt.Errorf("iniciar transacao migracao %s: %w", name, err)
		}
		if _, err := tx.Exec(string(content)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("executar migracao %s: %w", name, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("registrar migracao %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("confirmar migracao %s: %w", name, err)
		}
	}

	return nil
}

func seedDefaults(conn *sql.DB) error {
	_, err := conn.Exec(`
		INSERT OR IGNORE INTO people (name, email, is_self, color) VALUES
			('Eu', '', 1, '#212529'),
			('Outros', '', 0, '#6c757d');
		INSERT OR IGNORE INTO settings (key, value) VALUES
			('backup_reminder_days', '14'),
			('ai_provider', 'gemini'),
			('ai_model', 'gemini-2.5-flash'),
			('smtp_tls_mode', 'starttls');
	`)
	if err != nil {
		return fmt.Errorf("seed inicial: %w", err)
	}
	return nil
}
