package api

import (
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
	"github.com/FelipeSrutkoske/Skatt/internal/parser"
	"github.com/FelipeSrutkoske/Skatt/internal/store"
)

type setSplitsRequest struct {
	Splits []domain.SplitInput `json:"splits"`
}

type updateTransactionRequest struct {
	PurchaseDate *string `json:"purchase_date"`
	Description  *string `json:"description"`
	AmountCents  *int64  `json:"amount_cents"`
	IsIgnored    *bool   `json:"is_ignored"`
}

type assignRemainingRequest struct {
	PersonID int64 `json:"person_id"`
}

type batchAssignResponse struct {
	Updated int `json:"updated"`
}

func (s *Server) handleGetStatement(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	detail, err := s.store.GetStatementDetail(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleDeleteStatement(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	storedPath, err := s.store.DeleteStatement(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if storedPath != "" {
		s.removeUploadedFile(storedPath)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDownloadStatementFile(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	stmt, err := s.store.GetStatement(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if strings.TrimSpace(stmt.StoredPath) == "" {
		writeError(w, http.StatusNotFound, "fatura sem arquivo original anexado")
		return
	}

	targetPath, ok := s.resolveUploadPath(stmt.StoredPath)
	if !ok {
		writeError(w, http.StatusNotFound, "arquivo original indisponivel")
		return
	}

	info, err := os.Stat(targetPath)
	if err != nil || info.IsDir() {
		if errors.Is(err, os.ErrNotExist) || err == nil {
			writeError(w, http.StatusNotFound, "arquivo original nao encontrado")
			return
		}
		writeError(w, http.StatusInternalServerError, "erro ao acessar arquivo original")
		return
	}

	downloadName := stmt.OriginalFilename
	if strings.TrimSpace(downloadName) == "" {
		downloadName = filepath.Base(targetPath)
	}
	downloadName = parser.SanitizeFilename(downloadName)
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": downloadName})
	if disposition != "" {
		w.Header().Set("Content-Disposition", disposition)
	}
	http.ServeFile(w, r, targetPath)
}

func (s *Server) handleSetTransactionSplits(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req setSplitsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := s.store.SetTransactionSplits(r.Context(), id, req.Splits)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tx)
}

func (s *Server) handleUpdateTransaction(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req updateTransactionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	in := store.UpdateTransactionInput{
		PurchaseDate: req.PurchaseDate,
		Description:  req.Description,
		AmountCents:  req.AmountCents,
		IsIgnored:    req.IsIgnored,
	}
	if req.Description != nil {
		norm, cur, tot := parser.NormalizeMerchantAndInstallment(*req.Description)
		in.NormalizedMerchant = &norm
		in.InstallmentCurrent = cur
		in.InstallmentTotal = tot
		if cur == nil && tot == nil {
			in.ClearInstallments = true
		}
	}

	tx, err := s.store.UpdateTransaction(r.Context(), id, in)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tx)
}

func (s *Server) handleApplySuggestions(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	updated, err := s.store.ApplySuggestions(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, batchAssignResponse{Updated: updated})
}

func (s *Server) handleAssignRemaining(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req assignRemainingRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.PersonID <= 0 {
		writeError(w, http.StatusBadRequest, "person_id invalido")
		return
	}

	updated, err := s.store.AssignRemaining(r.Context(), id, req.PersonID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, batchAssignResponse{Updated: updated})
}
