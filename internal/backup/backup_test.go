package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FelipeSrutkoske/Skatt/internal/db"
)

type stubStore struct {
	mu   sync.RWMutex
	conn *sql.DB
}

func newStubStore(t *testing.T, dbPath string) *stubStore {
	t.Helper()
	conn, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("abrir banco de teste: %v", err)
	}
	st := &stubStore{conn: conn}
	t.Cleanup(func() {
		if current := st.DB(); current != nil {
			_ = current.Close()
		}
	})
	return st
}

func (s *stubStore) DB() *sql.DB {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.conn
}

func (s *stubStore) ReplaceDB(conn *sql.DB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conn = conn
}

func (s *stubStore) GetSettings(ctx context.Context) (map[string]string, error) {
	conn := s.DB()
	rows, err := conn.QueryContext(ctx, "SELECT key, value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		settings[k] = v
	}
	return settings, rows.Err()
}

func (s *stubStore) SetSettings(ctx context.Context, kv map[string]string) error {
	conn := s.DB()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for k, v := range kv {
		_, err := tx.ExecContext(ctx,
			"INSERT INTO settings (key, value, updated_at) VALUES (?, ?, datetime('now')) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')",
			k, v,
		)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *stubStore) TouchBackup(ctx context.Context) error {
	return s.SetSettings(ctx, map[string]string{
		"last_backup_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func TestExportExcludesSecretsByDefaultAndIncludesWhenRequested(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	uploadsDir := filepath.Join(dataDir, "uploads")
	st := newStubStore(t, filepath.Join(dataDir, "finance.db"))

	if err := st.SetSettings(ctx, map[string]string{
		"smtp_host":  "smtp.gmail.com",
		"smtp_pass":  "segredo-smtp-123",
		"ai_api_key": "segredo-gemini-456",
	}); err != nil {
		t.Fatalf("salvar settings: %v", err)
	}

	uploadFile := filepath.Join(uploadsDir, "2025", "04", "1_nubank.csv")
	if err := os.MkdirAll(filepath.Dir(uploadFile), 0o755); err != nil {
		t.Fatalf("criar diretorio de upload: %v", err)
	}
	uploadContent := "date,title,amount\n2025-04-10,UBER *TRIP,19.90\n"
	if err := os.WriteFile(uploadFile, []byte(uploadContent), 0o644); err != nil {
		t.Fatalf("gravar arquivo de upload: %v", err)
	}

	var bufNoSecrets bytes.Buffer
	if err := ExportZip(ctx, st, uploadsDir, false, &bufNoSecrets); err != nil {
		t.Fatalf("ExportZip(includeSecrets=false): %v", err)
	}

	currentSettings, err := st.GetSettings(ctx)
	if err != nil {
		t.Fatalf("ler settings apos exportacao: %v", err)
	}
	if currentSettings["last_backup_at"] == "" {
		t.Fatalf("esperava last_backup_at atualizado")
	}
	if currentSettings["smtp_pass"] != "segredo-smtp-123" || currentSettings["ai_api_key"] != "segredo-gemini-456" {
		t.Fatalf("exportacao nao deve alterar credenciais no banco ativo")
	}

	manifestNoSecrets, settingsNoSecrets, uploadsNoSecrets := inspectBackupZip(t, bufNoSecrets.Bytes())
	if manifestNoSecrets.Version != 1 {
		t.Fatalf("versao do manifest esperada 1, obtida %d", manifestNoSecrets.Version)
	}
	if manifestNoSecrets.ExportedAt == "" {
		t.Fatalf("exported_at vazio no manifest")
	}
	if manifestNoSecrets.IncludesSecrets {
		t.Fatalf("esperava includes_secrets=false")
	}
	if settingsNoSecrets["smtp_host"] != "smtp.gmail.com" {
		t.Fatalf("smtp_host esperado smtp.gmail.com, obtido %q", settingsNoSecrets["smtp_host"])
	}
	if _, exists := settingsNoSecrets["smtp_pass"]; exists {
		t.Fatalf("smtp_pass nao deveria estar presente quando includeSecrets=false")
	}
	if _, exists := settingsNoSecrets["ai_api_key"]; exists {
		t.Fatalf("ai_api_key nao deveria estar presente quando includeSecrets=false")
	}
	if got := uploadsNoSecrets["uploads/2025/04/1_nubank.csv"]; got != uploadContent {
		t.Fatalf("conteudo de upload divergente: %q", got)
	}

	var bufWithSecrets bytes.Buffer
	if err := ExportZip(ctx, st, uploadsDir, true, &bufWithSecrets); err != nil {
		t.Fatalf("ExportZip(includeSecrets=true): %v", err)
	}

	manifestWithSecrets, settingsWithSecrets, _ := inspectBackupZip(t, bufWithSecrets.Bytes())
	if !manifestWithSecrets.IncludesSecrets {
		t.Fatalf("esperava includes_secrets=true")
	}
	if settingsWithSecrets["smtp_pass"] != "segredo-smtp-123" {
		t.Fatalf("smtp_pass esperado no backup com segredos, obtido %q", settingsWithSecrets["smtp_pass"])
	}
	if settingsWithSecrets["ai_api_key"] != "segredo-gemini-456" {
		t.Fatalf("ai_api_key esperado no backup com segredos, obtido %q", settingsWithSecrets["ai_api_key"])
	}
}

func TestRestorePreservesLocalSecretsAndCreatesPreRestoreBackup(t *testing.T) {
	ctx := context.Background()

	sourceDir := t.TempDir()
	sourceUploads := filepath.Join(sourceDir, "uploads")
	sourceStore := newStubStore(t, filepath.Join(sourceDir, "finance.db"))

	if _, err := sourceStore.DB().ExecContext(ctx, "INSERT INTO people (name, email, is_self, color) VALUES ('Regina', 'regina@example.com', 0, '#334155')"); err != nil {
		t.Fatalf("inserir pessoa na origem: %v", err)
	}
	if _, err := sourceStore.DB().ExecContext(ctx,
		"INSERT INTO statements (bank, card_label, reference_year, reference_month, original_filename, stored_path, file_sha256, file_format, parser_used) VALUES ('nubank', 'Principal', 2025, 4, 'fatura.csv', '2025/04/1_fatura.csv', 'abc123', 'csv', 'nubank_csv')",
	); err != nil {
		t.Fatalf("inserir fatura na origem: %v", err)
	}
	if err := sourceStore.SetSettings(ctx, map[string]string{
		"smtp_host":  "smtp.backup.local",
		"smtp_pass":  "origem-nao-exportada",
		"ai_api_key": "origem-ai-nao-exportada",
	}); err != nil {
		t.Fatalf("configurar settings na origem: %v", err)
	}

	sourceUploadFile := filepath.Join(sourceUploads, "2025", "04", "1_fatura.csv")
	if err := os.MkdirAll(filepath.Dir(sourceUploadFile), 0o755); err != nil {
		t.Fatalf("criar pasta de upload na origem: %v", err)
	}
	expectedUploadContent := "date,title,amount\n2025-04-12,MERCADO,120.00\n"
	if err := os.WriteFile(sourceUploadFile, []byte(expectedUploadContent), 0o644); err != nil {
		t.Fatalf("gravar upload na origem: %v", err)
	}

	backupZipPath := filepath.Join(t.TempDir(), "backup.zip")
	zipFile, err := os.Create(backupZipPath)
	if err != nil {
		t.Fatalf("criar arquivo zip: %v", err)
	}
	if err := ExportZip(ctx, sourceStore, sourceUploads, false, zipFile); err != nil {
		_ = zipFile.Close()
		t.Fatalf("exportar backup de origem: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("fechar zip de origem: %v", err)
	}

	targetDir := t.TempDir()
	targetStore := newStubStore(t, filepath.Join(targetDir, "finance.db"))

	if _, err := targetStore.DB().ExecContext(ctx, "INSERT INTO people (name, email, is_self, color) VALUES ('Carlos', 'carlos@example.com', 0, '#0f172a')"); err != nil {
		t.Fatalf("inserir pessoa local: %v", err)
	}
	if err := targetStore.SetSettings(ctx, map[string]string{
		"smtp_host":  "smtp.local.antigo",
		"smtp_pass":  "senha-local-preservada",
		"ai_api_key": "chave-ai-local-preservada",
	}); err != nil {
		t.Fatalf("configurar credenciais locais: %v", err)
	}

	oldUploadFile := filepath.Join(targetDir, "uploads", "2024", "01", "antigo.csv")
	if err := os.MkdirAll(filepath.Dir(oldUploadFile), 0o755); err != nil {
		t.Fatalf("criar upload antigo: %v", err)
	}
	if err := os.WriteFile(oldUploadFile, []byte("antigo"), 0o644); err != nil {
		t.Fatalf("gravar upload antigo: %v", err)
	}

	if err := RestoreZip(ctx, targetStore, targetDir, backupZipPath); err != nil {
		t.Fatalf("RestoreZip: %v", err)
	}

	var reginaCount, carlosCount, stmtCount int
	if err := targetStore.DB().QueryRowContext(ctx, "SELECT COUNT(1) FROM people WHERE name = 'Regina'").Scan(&reginaCount); err != nil {
		t.Fatalf("consultar Regina: %v", err)
	}
	if err := targetStore.DB().QueryRowContext(ctx, "SELECT COUNT(1) FROM people WHERE name = 'Carlos'").Scan(&carlosCount); err != nil {
		t.Fatalf("consultar Carlos: %v", err)
	}
	if err := targetStore.DB().QueryRowContext(ctx, "SELECT COUNT(1) FROM statements WHERE bank = 'nubank'").Scan(&stmtCount); err != nil {
		t.Fatalf("consultar statements: %v", err)
	}
	if reginaCount != 1 || carlosCount != 0 || stmtCount != 1 {
		t.Fatalf("dados restaurados inesperados: regina=%d carlos=%d statements=%d", reginaCount, carlosCount, stmtCount)
	}

	restoredSettings, err := targetStore.GetSettings(ctx)
	if err != nil {
		t.Fatalf("ler settings restaurados: %v", err)
	}
	if restoredSettings["smtp_host"] != "smtp.backup.local" {
		t.Fatalf("smtp_host esperado smtp.backup.local, obtido %q", restoredSettings["smtp_host"])
	}
	if restoredSettings["smtp_pass"] != "senha-local-preservada" {
		t.Fatalf("smtp_pass local nao preservado: %q", restoredSettings["smtp_pass"])
	}
	if restoredSettings["ai_api_key"] != "chave-ai-local-preservada" {
		t.Fatalf("ai_api_key local nao preservado: %q", restoredSettings["ai_api_key"])
	}

	restoredUploadBytes, err := os.ReadFile(filepath.Join(targetDir, "uploads", "2025", "04", "1_fatura.csv"))
	if err != nil {
		t.Fatalf("ler upload restaurado: %v", err)
	}
	if string(restoredUploadBytes) != expectedUploadContent {
		t.Fatalf("conteudo do upload restaurado divergente: %q", string(restoredUploadBytes))
	}
	if _, err := os.Stat(oldUploadFile); !os.IsNotExist(err) {
		t.Fatalf("upload antigo deveria ter sido removido apos restauracao")
	}

	preRestoreMatches, err := filepath.Glob(filepath.Join(targetDir, "backups", "pre-restore-*.db"))
	if err != nil {
		t.Fatalf("buscar pre-restore backup: %v", err)
	}
	if len(preRestoreMatches) != 1 {
		t.Fatalf("esperava 1 arquivo pre-restore-*.db, encontrado %d", len(preRestoreMatches))
	}

	preConn, err := sql.Open("sqlite", preRestoreMatches[0])
	if err != nil {
		t.Fatalf("abrir copia pre-restore: %v", err)
	}
	defer preConn.Close()

	var preCarlosCount int
	if err := preConn.QueryRowContext(ctx, "SELECT COUNT(1) FROM people WHERE name = 'Carlos'").Scan(&preCarlosCount); err != nil {
		t.Fatalf("consultar copia pre-restore: %v", err)
	}
	if preCarlosCount != 1 {
		t.Fatalf("esperava Carlos presente na copia pre-restore, obtido %d", preCarlosCount)
	}
}

func TestRestoreRejectsZipSlipAndCorruptDB(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	st := newStubStore(t, filepath.Join(dataDir, "finance.db"))

	if _, err := st.DB().ExecContext(ctx, "INSERT INTO people (name, email, is_self, color) VALUES ('Intacto', 'intacto@example.com', 0, '#1e293b')"); err != nil {
		t.Fatalf("inserir pessoa de controle: %v", err)
	}

	manifestJSON, err := json.Marshal(Manifest{
		Version:         1,
		ExportedAt:      time.Now().UTC().Format(time.RFC3339),
		IncludesSecrets: false,
	})
	if err != nil {
		t.Fatalf("serializar manifest: %v", err)
	}

	t.Run("zip_slip", func(t *testing.T) {
		zipPath := filepath.Join(t.TempDir(), "zipslip.zip")
		writeCustomZip(t, zipPath, map[string][]byte{
			"manifest.json":  manifestJSON,
			"finance.db":     readDBSnapshotBytes(t, st.DB()),
			"../../evil.txt": []byte("pwned"),
		})

		if err := RestoreZip(ctx, st, dataDir, zipPath); err == nil {
			t.Fatalf("esperava erro ao restaurar zip com path traversal")
		}

		evilPath := filepath.Clean(filepath.Join(dataDir, "..", "..", "evil.txt"))
		if _, err := os.Stat(evilPath); !os.IsNotExist(err) {
			t.Fatalf("arquivo fora do diretorio nao deveria existir: %s", evilPath)
		}
		assertControlPersonIntact(t, st)
	})

	t.Run("corrupt_db", func(t *testing.T) {
		zipPath := filepath.Join(t.TempDir(), "corrupt.zip")
		writeCustomZip(t, zipPath, map[string][]byte{
			"manifest.json": manifestJSON,
			"finance.db":    []byte("nao e um banco sqlite valido"),
		})

		if err := RestoreZip(ctx, st, dataDir, zipPath); err == nil {
			t.Fatalf("esperava erro ao restaurar zip com finance.db corrompido")
		}
		assertControlPersonIntact(t, st)
	})

	t.Run("missing_statements_table", func(t *testing.T) {
		emptyDBPath := filepath.Join(t.TempDir(), "empty.db")
		emptyConn, err := sql.Open("sqlite", emptyDBPath)
		if err != nil {
			t.Fatalf("criar sqlite vazio: %v", err)
		}
		if _, err := emptyConn.Exec("CREATE TABLE dummy (id INTEGER PRIMARY KEY)"); err != nil {
			_ = emptyConn.Close()
			t.Fatalf("criar tabela dummy: %v", err)
		}
		if err := emptyConn.Close(); err != nil {
			t.Fatalf("fechar sqlite vazio: %v", err)
		}
		emptyDBBytes, err := os.ReadFile(emptyDBPath)
		if err != nil {
			t.Fatalf("ler sqlite vazio: %v", err)
		}

		zipPath := filepath.Join(t.TempDir(), "missing_table.zip")
		writeCustomZip(t, zipPath, map[string][]byte{
			"manifest.json": manifestJSON,
			"finance.db":    emptyDBBytes,
		})

		if err := RestoreZip(ctx, st, dataDir, zipPath); err == nil {
			t.Fatalf("esperava erro ao restaurar banco sem tabela statements")
		}
		assertControlPersonIntact(t, st)
	})
}

func assertControlPersonIntact(t *testing.T, st *stubStore) {
	t.Helper()
	var count int
	if err := st.DB().QueryRow("SELECT COUNT(1) FROM people WHERE name = 'Intacto'").Scan(&count); err != nil {
		t.Fatalf("consultar pessoa de controle: %v", err)
	}
	if count != 1 {
		t.Fatalf("banco ativo foi alterado indevidamente: count=%d", count)
	}
}

func readDBSnapshotBytes(t *testing.T, conn *sql.DB) []byte {
	t.Helper()
	snapPath := filepath.Join(t.TempDir(), "snap.db")
	if _, err := conn.Exec("VACUUM INTO ?", snapPath); err != nil {
		t.Fatalf("criar snapshot para teste: %v", err)
	}
	raw, err := os.ReadFile(snapPath)
	if err != nil {
		t.Fatalf("ler snapshot para teste: %v", err)
	}
	return raw
}

func writeCustomZip(t *testing.T, zipPath string, entries map[string][]byte) {
	t.Helper()
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("criar zip customizado: %v", err)
	}
	zw := zip.NewWriter(f)
	for name, content := range entries {
		fw, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			_ = f.Close()
			t.Fatalf("criar entrada %s: %v", name, err)
		}
		if _, err := fw.Write(content); err != nil {
			_ = zw.Close()
			_ = f.Close()
			t.Fatalf("gravar entrada %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		t.Fatalf("fechar writer zip: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("fechar arquivo zip: %v", err)
	}
}

func inspectBackupZip(t *testing.T, zipBytes []byte) (Manifest, map[string]string, map[string]string) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("abrir zip gerado: %v", err)
	}

	var manifest Manifest
	var dbBytes []byte
	uploads := make(map[string]string)

	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("abrir entrada %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("ler entrada %s: %v", f.Name, err)
		}

		switch {
		case f.Name == "manifest.json":
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Fatalf("decodificar manifest: %v", err)
			}
		case f.Name == "finance.db":
			dbBytes = data
		case strings.HasPrefix(f.Name, "uploads/"):
			uploads[f.Name] = string(data)
		}
	}

	if len(dbBytes) == 0 {
		t.Fatalf("finance.db nao encontrado no zip")
	}

	extractedPath := filepath.Join(t.TempDir(), "inspected.db")
	if err := os.WriteFile(extractedPath, dbBytes, 0o600); err != nil {
		t.Fatalf("gravar finance.db extraido: %v", err)
	}

	conn, err := sql.Open("sqlite", extractedPath)
	if err != nil {
		t.Fatalf("abrir finance.db extraido: %v", err)
	}
	defer conn.Close()

	rows, err := conn.Query("SELECT key, value FROM settings")
	if err != nil {
		t.Fatalf("consultar settings no snapshot: %v", err)
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatalf("ler linha settings: %v", err)
		}
		settings[k] = v
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar settings: %v", err)
	}

	return manifest, settings, uploads
}
