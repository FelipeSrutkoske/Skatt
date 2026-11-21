package backup

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/FelipeSrutkoske/Skatt/internal/db"
)

const manifestVersion = 1

var secretSettingKeys = []string{"smtp_pass", "ai_api_key"}

type Store interface {
	DB() *sql.DB
	ReplaceDB(conn *sql.DB)
	GetSettings(ctx context.Context) (map[string]string, error)
	SetSettings(ctx context.Context, kv map[string]string) error
	TouchBackup(ctx context.Context) error
}

type Manifest struct {
	Version         int    `json:"version"`
	ExportedAt      string `json:"exported_at"`
	IncludesSecrets bool   `json:"includes_secrets"`
}

func ExportZip(ctx context.Context, st Store, uploadsDir string, includeSecrets bool, w io.Writer) error {
	if st == nil || st.DB() == nil {
		return errors.New("store invalido para exportacao de backup")
	}
	if w == nil {
		return errors.New("writer invalido para exportacao de backup")
	}

	tmpDir, err := os.MkdirTemp("", "finance-export-*")
	if err != nil {
		return fmt.Errorf("criar diretorio temporario de exportacao: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	snapshotPath := filepath.Join(tmpDir, "finance.db")
	if _, err := st.DB().ExecContext(ctx, "VACUUM INTO ?", snapshotPath); err != nil {
		return fmt.Errorf("criar snapshot sqlite: %w", err)
	}

	if !includeSecrets {
		if err := scrubSnapshotSecrets(ctx, snapshotPath); err != nil {
			return fmt.Errorf("remover credenciais do snapshot: %w", err)
		}
	}

	manifest := Manifest{
		Version:         manifestVersion,
		ExportedAt:      time.Now().UTC().Format(time.RFC3339),
		IncludesSecrets: includeSecrets,
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("serializar manifest: %w", err)
	}

	zw := zip.NewWriter(w)

	if err := writeBytesToZip(zw, "manifest.json", manifestBytes); err != nil {
		_ = zw.Close()
		return err
	}

	if err := writeFileToZip(zw, "finance.db", snapshotPath); err != nil {
		_ = zw.Close()
		return err
	}

	if err := writeUploadsToZip(zw, uploadsDir); err != nil {
		_ = zw.Close()
		return err
	}

	if err := zw.Close(); err != nil {
		return fmt.Errorf("finalizar arquivo zip: %w", err)
	}

	if err := st.TouchBackup(ctx); err != nil {
		return fmt.Errorf("atualizar data de backup: %w", err)
	}

	return nil
}

func RestoreZip(ctx context.Context, st Store, dataDir string, zipPath string) error {
	if st == nil || st.DB() == nil {
		return errors.New("store invalido para restauracao de backup")
	}
	if strings.TrimSpace(dataDir) == "" {
		return errors.New("diretorio de dados invalido")
	}
	if strings.TrimSpace(zipPath) == "" {
		return errors.New("caminho do arquivo zip invalido")
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("abrir arquivo zip de backup: %w", err)
	}
	defer zr.Close()

	extractDir, err := os.MkdirTemp("", "finance-restore-*")
	if err != nil {
		return fmt.Errorf("criar diretorio temporario de restauracao: %w", err)
	}
	defer os.RemoveAll(extractDir)

	if err := extractAndValidateZip(&zr.Reader, extractDir); err != nil {
		return err
	}

	manifest, err := readManifest(filepath.Join(extractDir, "manifest.json"))
	if err != nil {
		return err
	}

	extractedDBPath := filepath.Join(extractDir, "finance.db")
	if err := validateSQLiteSnapshot(ctx, extractedDBPath); err != nil {
		return err
	}

	localSettings, err := st.GetSettings(ctx)
	if err != nil {
		return fmt.Errorf("ler configuracoes locais: %w", err)
	}
	preservedSecrets := make(map[string]string)
	for _, key := range secretSettingKeys {
		if val, ok := localSettings[key]; ok && val != "" {
			preservedSecrets[key] = val
		}
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("criar diretorio de dados: %w", err)
	}

	preRestorePath, err := createPreRestoreBackup(ctx, st.DB(), dataDir)
	if err != nil {
		return err
	}

	stagingDBPath := filepath.Join(dataDir, ".finance.db.restore.tmp")
	defer os.Remove(stagingDBPath)
	if err := copyFile(extractedDBPath, stagingDBPath, 0o600); err != nil {
		return fmt.Errorf("preparar banco para substituicao: %w", err)
	}

	stagingUploadsDir := filepath.Join(dataDir, ".uploads.restore.tmp")
	_ = os.RemoveAll(stagingUploadsDir)
	defer os.RemoveAll(stagingUploadsDir)
	if err := os.MkdirAll(stagingUploadsDir, 0o755); err != nil {
		return fmt.Errorf("preparar diretorio de uploads: %w", err)
	}
	extractedUploadsDir := filepath.Join(extractDir, "uploads")
	if info, statErr := os.Stat(extractedUploadsDir); statErr == nil && info.IsDir() {
		if err := copyDirContents(extractedUploadsDir, stagingUploadsDir); err != nil {
			return fmt.Errorf("copiar uploads do backup: %w", err)
		}
	}

	targetDBPath := filepath.Join(dataDir, "finance.db")
	targetUploadsDir := filepath.Join(dataDir, "uploads")
	oldUploadsBackupDir := filepath.Join(dataDir, ".uploads.pre-restore.tmp")
	_ = os.RemoveAll(oldUploadsBackupDir)

	oldDB := st.DB()
	if err := oldDB.Close(); err != nil {
		return fmt.Errorf("fechar banco atual: %w", err)
	}

	removeSQLiteSidecars(targetDBPath)

	if err := os.Rename(stagingDBPath, targetDBPath); err != nil {
		reopenOriginalDB(st, targetDBPath, preRestorePath)
		return fmt.Errorf("substituir arquivo do banco: %w", err)
	}

	newDB, err := db.Open(targetDBPath)
	if err != nil {
		rollbackDatabase(st, targetDBPath, preRestorePath)
		return fmt.Errorf("abrir banco restaurado: %w", err)
	}

	hasOldUploads := false
	if _, statErr := os.Stat(targetUploadsDir); statErr == nil {
		if err := os.Rename(targetUploadsDir, oldUploadsBackupDir); err != nil {
			_ = newDB.Close()
			rollbackDatabase(st, targetDBPath, preRestorePath)
			return fmt.Errorf("mover uploads antigos: %w", err)
		}
		hasOldUploads = true
	}

	if err := os.Rename(stagingUploadsDir, targetUploadsDir); err != nil {
		if hasOldUploads {
			_ = os.Rename(oldUploadsBackupDir, targetUploadsDir)
		}
		_ = newDB.Close()
		rollbackDatabase(st, targetDBPath, preRestorePath)
		return fmt.Errorf("substituir diretorio de uploads: %w", err)
	}

	if hasOldUploads {
		_ = os.RemoveAll(oldUploadsBackupDir)
	}

	st.ReplaceDB(newDB)

	if !manifest.IncludesSecrets && len(preservedSecrets) > 0 {
		if err := st.SetSettings(ctx, preservedSecrets); err != nil {
			return fmt.Errorf("restaurar credenciais locais preservadas: %w", err)
		}
	}

	return nil
}

func scrubSnapshotSecrets(ctx context.Context, snapshotPath string) error {
	conn, err := sql.Open("sqlite", snapshotPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	if _, err := conn.ExecContext(ctx, "DELETE FROM settings WHERE key IN ('smtp_pass', 'ai_api_key')"); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "VACUUM"); err != nil {
		return err
	}
	return conn.Close()
}

func writeBytesToZip(zw *zip.Writer, entryName string, data []byte) error {
	fw, err := zw.Create(entryName)
	if err != nil {
		return fmt.Errorf("criar entrada %s no zip: %w", entryName, err)
	}
	if _, err := fw.Write(data); err != nil {
		return fmt.Errorf("gravar entrada %s no zip: %w", entryName, err)
	}
	return nil
}

func writeFileToZip(zw *zip.Writer, entryName string, srcPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("abrir arquivo %s: %w", srcPath, err)
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return fmt.Errorf("ler metadados de %s: %w", srcPath, err)
	}

	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return fmt.Errorf("criar cabecalho zip para %s: %w", entryName, err)
	}
	hdr.Name = entryName
	hdr.Method = zip.Deflate

	fw, err := zw.CreateHeader(hdr)
	if err != nil {
		return fmt.Errorf("criar entrada %s no zip: %w", entryName, err)
	}
	if _, err := io.Copy(fw, src); err != nil {
		return fmt.Errorf("gravar arquivo %s no zip: %w", entryName, err)
	}
	return nil
}

func writeUploadsToZip(zw *zip.Writer, uploadsDir string) error {
	if strings.TrimSpace(uploadsDir) == "" {
		return nil
	}
	info, err := os.Stat(uploadsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("verificar diretorio de uploads: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("caminho de uploads nao e diretorio: %s", uploadsDir)
	}

	return filepath.WalkDir(uploadsDir, func(currentPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(uploadsDir, currentPath)
		if err != nil {
			return err
		}
		zipEntryName := path.Join("uploads", filepath.ToSlash(rel))
		return writeFileToZip(zw, zipEntryName, currentPath)
	})
}

func extractAndValidateZip(zr *zip.Reader, extractDir string) error {
	hasManifest := false
	hasDB := false
	seen := make(map[string]struct{}, len(zr.File))

	for _, f := range zr.File {
		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("link simbolico nao permitido no backup: %q", f.Name)
		}

		cleaned, err := validateZipEntryName(f.Name)
		if err != nil {
			return err
		}

		if f.FileInfo().IsDir() {
			if cleaned == "manifest.json" || cleaned == "finance.db" {
				return fmt.Errorf("entrada %s nao pode ser diretorio", cleaned)
			}
			continue
		}

		if cleaned == "uploads" {
			return errors.New("entrada uploads deve ser diretorio")
		}

		if _, exists := seen[cleaned]; exists {
			return fmt.Errorf("entrada duplicada no backup: %s", cleaned)
		}
		seen[cleaned] = struct{}{}

		if cleaned == "manifest.json" {
			hasManifest = true
		}
		if cleaned == "finance.db" {
			hasDB = true
		}

		destPath := filepath.Join(extractDir, filepath.FromSlash(cleaned))
		rel, err := filepath.Rel(extractDir, destPath)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("path traversal detectado no backup: %q", f.Name)
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return fmt.Errorf("criar diretorio para %s: %w", cleaned, err)
		}

		if err := extractZipFile(f, destPath); err != nil {
			return err
		}
	}

	if !hasManifest {
		return errors.New("arquivo manifest.json ausente no backup")
	}
	if !hasDB {
		return errors.New("arquivo finance.db ausente no backup")
	}

	return nil
}

func validateZipEntryName(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\x00') || strings.Contains(name, "\\") {
		return "", fmt.Errorf("entrada de backup invalida: %q", name)
	}
	if strings.HasPrefix(name, "/") || filepath.IsAbs(name) {
		return "", fmt.Errorf("caminho absoluto nao permitido no backup: %q", name)
	}
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" {
		return "", fmt.Errorf("entrada de backup invalida: %q", name)
	}
	for _, segment := range strings.Split(trimmed, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("path traversal detectado no backup: %q", name)
		}
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "manifest.json" || cleaned == "finance.db" || cleaned == "uploads" || strings.HasPrefix(cleaned, "uploads/") {
		return cleaned, nil
	}
	return "", fmt.Errorf("entrada nao permitida no backup: %q", name)
}

func extractZipFile(f *zip.File, destPath string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("abrir entrada %s do zip: %w", f.Name, err)
	}
	defer rc.Close()

	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("criar arquivo extraido %s: %w", f.Name, err)
	}

	if _, err := io.Copy(dst, rc); err != nil {
		_ = dst.Close()
		return fmt.Errorf("extrair entrada %s do zip: %w", f.Name, err)
	}
	if err := dst.Close(); err != nil {
		return fmt.Errorf("fechar arquivo extraido %s: %w", f.Name, err)
	}
	return nil
}

func readManifest(manifestPath string) (Manifest, error) {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("ler manifest.json: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, fmt.Errorf("manifest.json invalido: %w", err)
	}
	if m.Version < 1 {
		return Manifest{}, fmt.Errorf("versao de manifest invalida: %d", m.Version)
	}
	return m, nil
}

func validateSQLiteSnapshot(ctx context.Context, dbPath string) error {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("abrir banco do backup: %w", err)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	rows, err := conn.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return fmt.Errorf("banco do backup corrompido: %w", err)
	}
	defer rows.Close()

	checked := false
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return fmt.Errorf("ler resultado de integrity_check: %w", err)
		}
		if result != "ok" {
			return fmt.Errorf("banco do backup falhou em integrity_check: %s", result)
		}
		checked = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("erro ao verificar integridade do banco: %w", err)
	}
	if !checked {
		return errors.New("integrity_check nao retornou resultado")
	}

	var tableCount int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = 'statements'").Scan(&tableCount); err != nil {
		return fmt.Errorf("verificar esquema do banco de backup: %w", err)
	}
	if tableCount != 1 {
		return errors.New("banco do backup nao contem a tabela statements")
	}

	return nil
}

func createPreRestoreBackup(ctx context.Context, conn *sql.DB, dataDir string) (string, error) {
	backupsDir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(backupsDir, 0o755); err != nil {
		return "", fmt.Errorf("criar diretorio de backups pre-restore: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	preRestorePath := filepath.Join(backupsDir, fmt.Sprintf("pre-restore-%s.db", timestamp))
	for seq := 1; ; seq++ {
		if _, err := os.Stat(preRestorePath); errors.Is(err, os.ErrNotExist) {
			break
		}
		preRestorePath = filepath.Join(backupsDir, fmt.Sprintf("pre-restore-%s-%d.db", timestamp, seq))
	}

	if _, err := conn.ExecContext(ctx, "VACUUM INTO ?", preRestorePath); err != nil {
		return "", fmt.Errorf("criar copia pre-restore: %w", err)
	}

	return preRestorePath, nil
}

func removeSQLiteSidecars(dbPath string) {
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
}

func reopenOriginalDB(st Store, targetDBPath string, preRestorePath string) {
	if conn, err := db.Open(targetDBPath); err == nil {
		st.ReplaceDB(conn)
		return
	}
	rollbackDatabase(st, targetDBPath, preRestorePath)
}

func rollbackDatabase(st Store, targetDBPath string, preRestorePath string) {
	removeSQLiteSidecars(targetDBPath)
	_ = copyFile(preRestorePath, targetDBPath, 0o600)
	if conn, err := db.Open(targetDBPath); err == nil {
		st.ReplaceDB(conn)
	}
}

func copyFile(srcPath string, dstPath string, mode os.FileMode) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}

	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return err
	}
	return dst.Close()
}

func copyDirContents(srcDir string, dstDir string) error {
	return filepath.WalkDir(srcDir, func(currentPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcDir, currentPath)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		targetPath := filepath.Join(dstDir, rel)
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		return copyFile(currentPath, targetPath, 0o644)
	})
}
