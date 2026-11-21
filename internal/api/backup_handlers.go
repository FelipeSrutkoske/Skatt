package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/FelipeSrutkoske/Skatt/internal/backup"
)

const maxBackupUploadBytes = 100 << 20

func (s *Server) handleExportBackup(w http.ResponseWriter, r *http.Request) {
	includeSecrets := parseBoolQuery(r.URL.Query().Get("include_secrets"))

	tmpFile, err := os.CreateTemp("", "finance-backup-export-*.zip")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao preparar arquivo de backup")
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if err := backup.ExportZip(r.Context(), s.store, s.uploadsDir, includeSecrets, tmpFile); err != nil {
		_ = tmpFile.Close()
		writeError(w, http.StatusInternalServerError, "falha ao gerar backup")
		return
	}

	info, err := tmpFile.Stat()
	if err != nil {
		_ = tmpFile.Close()
		writeError(w, http.StatusInternalServerError, "falha ao inspecionar backup")
		return
	}

	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		_ = tmpFile.Close()
		writeError(w, http.StatusInternalServerError, "falha ao ler arquivo de backup")
		return
	}
	defer tmpFile.Close()

	filename := fmt.Sprintf("finance-backup-%s.zip", time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, tmpFile)
}

func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBackupUploadBytes+(1<<20))
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "arquivo de backup excede o limite de 100 MB")
			return
		}
		writeError(w, http.StatusBadRequest, "requisicao multipart invalida")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "campo file obrigatorio")
		return
	}
	defer file.Close()

	if header.Size > maxBackupUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "arquivo de backup excede o limite de 100 MB")
		return
	}

	tmpFile, err := os.CreateTemp("", "finance-backup-restore-*.zip")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao preparar arquivo temporario")
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	written, copyErr := io.Copy(tmpFile, io.LimitReader(file, maxBackupUploadBytes+1))
	closeErr := tmpFile.Close()
	if copyErr != nil || closeErr != nil {
		writeError(w, http.StatusInternalServerError, "falha ao gravar arquivo de backup")
		return
	}
	if written > maxBackupUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "arquivo de backup excede o limite de 100 MB")
		return
	}

	if err := backup.RestoreZip(r.Context(), s.store, s.dataDir, tmpPath); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, operationResult{
		OK:      true,
		Message: "Backup restaurado com sucesso.",
	})
}
