package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	reSafeID   = regexp.MustCompile(`^[a-zA-Z0-9\-]+$`)
	reSafeChar = regexp.MustCompile(`[^a-zA-Z0-9._\-]+`)
)

type StagingMeta struct {
	ID               string `json:"id"`
	OriginalFilename string `json:"original_filename"`
	SafeFilename     string `json:"safe_filename"`
	FilePath         string `json:"file_path"`
	FileSHA256       string `json:"file_sha256"`
	SizeBytes        int64  `json:"size_bytes"`
	CreatedAt        string `json:"created_at"`
}

type StagingManager struct {
	baseDir string
}

func NewStagingManager(baseDir string) *StagingManager {
	return &StagingManager{baseDir: baseDir}
}

func SanitizeFilename(filename string) string {
	cleaned := strings.ReplaceAll(filename, `\`, "/")
	base := filepath.Base(cleaned)
	base = strings.TrimSpace(stripAccents(base))
	base = strings.ReplaceAll(base, "..", "_")
	base = reSafeChar.ReplaceAllString(base, "_")
	base = strings.Trim(base, "._ ")
	if base == "" {
		return "upload.bin"
	}
	if len(base) > 120 {
		ext := filepath.Ext(base)
		if len(ext) > 16 {
			ext = ""
		}
		namePart := strings.TrimSuffix(base, ext)
		maxName := 120 - len(ext)
		if maxName < 1 {
			maxName = 1
		}
		if len(namePart) > maxName {
			namePart = namePart[:maxName]
		}
		base = namePart + ext
	}
	return base
}

func (m *StagingManager) Save(filename string, r io.Reader) (StagingMeta, error) {
	id := uuid.NewString()
	safeName := SanitizeFilename(filename)
	dir := filepath.Join(m.baseDir, id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return StagingMeta{}, err
	}

	filePath := filepath.Join(dir, safeName)
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		_ = os.RemoveAll(dir)
		return StagingMeta{}, err
	}

	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(f, hasher), r)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.RemoveAll(dir)
		return StagingMeta{}, copyErr
	}
	if closeErr != nil {
		_ = os.RemoveAll(dir)
		return StagingMeta{}, closeErr
	}

	meta := StagingMeta{
		ID:               id,
		OriginalFilename: filepath.Base(strings.ReplaceAll(filename, `\`, "/")),
		SafeFilename:     safeName,
		FilePath:         filePath,
		FileSHA256:       hex.EncodeToString(hasher.Sum(nil)),
		SizeBytes:        written,
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
	}

	metaBytes, err := json.Marshal(meta)
	if err != nil {
		_ = os.RemoveAll(dir)
		return StagingMeta{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), metaBytes, 0600); err != nil {
		_ = os.RemoveAll(dir)
		return StagingMeta{}, err
	}
	return meta, nil
}

func (m *StagingManager) LoadMeta(id string) (StagingMeta, error) {
	dir, err := m.stagingDir(id)
	if err != nil {
		return StagingMeta{}, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return StagingMeta{}, err
	}
	var meta StagingMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return StagingMeta{}, err
	}
	return meta, nil
}

func (m *StagingManager) SaveExtractedText(id, text string) error {
	dir, err := m.stagingDir(id)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "extracted.txt"), []byte(text), 0600)
}

func (m *StagingManager) ReadExtractedText(id string) (string, error) {
	dir, err := m.stagingDir(id)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, "extracted.txt"))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (m *StagingManager) PromoteToUploads(id, uploadsDir string, year, month int, statementID int64) (string, error) {
	meta, err := m.LoadMeta(id)
	if err != nil {
		return "", err
	}

	safeName := SanitizeFilename(meta.SafeFilename)
	relPath := fmt.Sprintf("%04d/%02d/%d_%s", year, month, statementID, safeName)
	targetPath := filepath.Join(uploadsDir, filepath.FromSlash(relPath))

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return "", err
	}

	if err := moveFile(meta.FilePath, targetPath); err != nil {
		return "", err
	}

	_ = m.Remove(id)
	return relPath, nil
}

func (m *StagingManager) Remove(id string) error {
	dir, err := m.stagingDir(id)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

func (m *StagingManager) CleanupExpired(maxAge time.Duration) error {
	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	now := time.Now().UTC()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		dir := filepath.Join(m.baseDir, id)

		var createdAt time.Time
		if meta, err := m.LoadMeta(id); err == nil {
			if parsed, err := time.Parse(time.RFC3339, meta.CreatedAt); err == nil {
				createdAt = parsed
			}
		}
		if createdAt.IsZero() {
			if info, err := entry.Info(); err == nil {
				createdAt = info.ModTime().UTC()
			}
		}
		if !createdAt.IsZero() && now.Sub(createdAt) > maxAge {
			_ = os.RemoveAll(dir)
		}
	}
	return nil
}

func (m *StagingManager) stagingDir(id string) (string, error) {
	if id == "" || !reSafeID.MatchString(id) {
		return "", fmt.Errorf("staging id invalido")
	}
	return filepath.Join(m.baseDir, id), nil
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return os.Remove(src)
}
