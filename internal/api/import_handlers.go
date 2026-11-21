package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
	"github.com/FelipeSrutkoske/Skatt/internal/parser"
	"github.com/FelipeSrutkoske/Skatt/internal/store"
)

const maxImportUploadBytes = 25 << 20

type unlockImportRequest struct {
	Password string `json:"password"`
}

type confirmImportTransactionInput struct {
	PurchaseDate string              `json:"purchase_date"`
	Description  string              `json:"description"`
	AmountCents  int64               `json:"amount_cents"`
	IsIgnored    bool                `json:"is_ignored"`
	Splits       []domain.SplitInput `json:"splits"`
}

type confirmImportRequest struct {
	StagingID          string                          `json:"staging_id"`
	Bank               string                          `json:"bank"`
	CardLabel          string                          `json:"card_label"`
	ReferenceYear      int                             `json:"reference_year"`
	ReferenceMonth     int                             `json:"reference_month"`
	DeclaredTotalCents *int64                          `json:"declared_total_cents"`
	ParserUsed         string                          `json:"parser_used"`
	ReplaceStatementID *int64                          `json:"replace_statement_id"`
	Transactions       []confirmImportTransactionInput `json:"transactions"`
}

func (s *Server) handlePreviewImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportUploadBytes+(1<<20))
	if err := r.ParseMultipartForm(maxImportUploadBytes); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "arquivo excede o limite de 25 MB")
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

	if header.Size > maxImportUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "arquivo excede o limite de 25 MB")
		return
	}

	meta, err := s.staging.Save(header.Filename, io.LimitReader(file, maxImportUploadBytes+1))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao armazenar arquivo temporario")
		return
	}
	if meta.SizeBytes > maxImportUploadBytes {
		_ = s.staging.Remove(meta.ID)
		writeError(w, http.StatusRequestEntityTooLarge, "arquivo excede o limite de 25 MB")
		return
	}

	outcome, err := parser.ParseFile(r.Context(), meta.OriginalFilename, meta.FilePath, "")
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "falha ao processar arquivo enviado")
		return
	}

	if outcome.ExtractedText != "" {
		_ = s.staging.SaveExtractedText(meta.ID, outcome.ExtractedText)
	}

	preview, err := s.buildPreviewResult(r.Context(), meta, outcome)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) handleUnlockImport(w http.ResponseWriter, r *http.Request) {
	stagingID := strings.TrimSpace(r.PathValue("stagingId"))
	if stagingID == "" {
		writeError(w, http.StatusBadRequest, "stagingId obrigatorio")
		return
	}

	var req unlockImportRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "senha obrigatoria")
		return
	}

	meta, err := s.staging.LoadMeta(stagingID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "arquivo temporario nao encontrado ou expirado")
			return
		}
		writeError(w, http.StatusBadRequest, "identificador de staging invalido")
		return
	}

	outcome, err := parser.ParseFile(r.Context(), meta.OriginalFilename, meta.FilePath, req.Password)
	if err != nil {
		if errors.Is(err, parser.ErrPDFInvalidPassword) {
			writeErrorWithCode(w, http.StatusUnprocessableEntity, "Senha incorreta para o PDF.", "invalid_password")
			return
		}
		writeError(w, http.StatusUnprocessableEntity, "nao foi possivel desbloquear o PDF")
		return
	}

	if outcome.ExtractedText != "" {
		_ = s.staging.SaveExtractedText(meta.ID, outcome.ExtractedText)
	}

	preview, err := s.buildPreviewResult(r.Context(), meta, outcome)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) handleAIExtractImport(w http.ResponseWriter, r *http.Request) {
	stagingID := strings.TrimSpace(r.PathValue("stagingId"))
	if stagingID == "" {
		writeError(w, http.StatusBadRequest, "stagingId obrigatorio")
		return
	}

	meta, err := s.staging.LoadMeta(stagingID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "arquivo temporario nao encontrado ou expirado")
			return
		}
		writeError(w, http.StatusBadRequest, "identificador de staging invalido")
		return
	}

	extractedText, err := s.staging.ReadExtractedText(meta.ID)
	if err != nil || strings.TrimSpace(extractedText) == "" {
		writeErrorWithCode(w, http.StatusUnprocessableEntity, "Arquivo nao possui texto extraivel para analise por IA.", "no_extractable_text")
		return
	}

	maskedText := parser.MaskPII(extractedText)

	settings, err := s.store.GetSettings(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	apiKey := strings.TrimSpace(settings["ai_api_key"])
	model := strings.TrimSpace(settings["ai_model"])
	if apiKey == "" {
		writeErrorWithCode(w, http.StatusBadRequest, "Chave de API de IA nao configurada em Configuracoes.", "ai_not_configured")
		return
	}

	extractor := s.aiExtractorFactory(apiKey, model)
	result, err := extractor.ExtractFromText(r.Context(), maskedText, meta.OriginalFilename)
	if err != nil {
		msg := strings.ReplaceAll(err.Error(), apiKey, "[REDACTED]")
		writeError(w, http.StatusBadGateway, msg)
		return
	}

	txs := make([]domain.PreviewTransaction, 0, len(result.Transactions))
	var computedTotal int64
	for i, item := range result.Transactions {
		desc := strings.TrimSpace(item.Description)
		norm := strings.TrimSpace(item.NormalizedMerchant)
		cur := item.InstallmentCurrent
		tot := item.InstallmentTotal
		if norm == "" || (cur == nil && tot == nil) {
			parsedNorm, parsedCur, parsedTot := parser.NormalizeMerchantAndInstallment(desc)
			if norm == "" {
				norm = parsedNorm
			}
			if cur == nil && tot == nil {
				cur = parsedCur
				tot = parsedTot
			}
		}

		splits := item.Splits
		if splits == nil {
			splits = make([]domain.SplitInput, 0)
		}

		suggestedID, err := s.store.LookupSuggestedPerson(r.Context(), norm)
		if err != nil {
			writeStoreError(w, err)
			return
		}

		if !item.IsIgnored {
			computedTotal += item.AmountCents
		}

		txs = append(txs, domain.PreviewTransaction{
			LineIndex:          i,
			PurchaseDate:       strings.TrimSpace(item.PurchaseDate),
			Description:        desc,
			NormalizedMerchant: norm,
			InstallmentCurrent: cur,
			InstallmentTotal:   tot,
			AmountCents:        item.AmountCents,
			IsIgnored:          item.IsIgnored,
			SuggestedPersonID:  suggestedID,
			Splits:             splits,
		})
	}

	bank := domain.NormalizeBank(result.Bank)
	dupWarning, err := s.store.FindDuplicates(r.Context(), meta.FileSHA256, bank, result.ReferenceYear, result.ReferenceMonth)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	preview := domain.PreviewResult{
		Status:             "parsed",
		StagingID:          meta.ID,
		OriginalFilename:   meta.OriginalFilename,
		FileFormat:         detectFormatFromFilename(meta.OriginalFilename),
		ParserUsed:         "ai_gemini",
		Bank:               bank,
		ReferenceYear:      result.ReferenceYear,
		ReferenceMonth:     result.ReferenceMonth,
		DeclaredTotalCents: result.DeclaredTotalCents,
		ComputedTotalCents: computedTotal,
		Transactions:       txs,
		DuplicateWarning:   dupWarning,
		HasExtractableText: true,
		TextPreview:        truncatePreviewText(maskedText),
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) handleConfirmImport(w http.ResponseWriter, r *http.Request) {
	var req confirmImportRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	stagingID := strings.TrimSpace(req.StagingID)
	var origFilename, fileSHA, fileFormat string
	if stagingID != "" {
		meta, err := s.staging.LoadMeta(stagingID)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, http.StatusNotFound, "arquivo temporario nao encontrado ou expirado")
				return
			}
			writeError(w, http.StatusBadRequest, "staging_id invalido")
			return
		}
		origFilename = meta.OriginalFilename
		fileSHA = meta.FileSHA256
		fileFormat = detectFormatFromFilename(meta.OriginalFilename)
	} else {
		fileFormat = "manual"
	}

	parserUsed := strings.TrimSpace(req.ParserUsed)
	if parserUsed == "" {
		parserUsed = "manual"
	}

	txs := make([]domain.PreviewTransaction, 0, len(req.Transactions))
	for i, item := range req.Transactions {
		desc := strings.TrimSpace(item.Description)
		norm, cur, tot := parser.NormalizeMerchantAndInstallment(desc)
		txs = append(txs, domain.PreviewTransaction{
			LineIndex:          i,
			PurchaseDate:       strings.TrimSpace(item.PurchaseDate),
			Description:        desc,
			NormalizedMerchant: norm,
			InstallmentCurrent: cur,
			InstallmentTotal:   tot,
			AmountCents:        item.AmountCents,
			IsIgnored:          item.IsIgnored,
			Splits:             item.Splits,
		})
	}

	stmt, oldStoredPath, err := s.store.CreateStatement(r.Context(), store.CreateStatementInput{
		Bank:               req.Bank,
		CardLabel:          req.CardLabel,
		ReferenceYear:      req.ReferenceYear,
		ReferenceMonth:     req.ReferenceMonth,
		OriginalFilename:   origFilename,
		StoredPath:         "",
		FileSHA256:         fileSHA,
		FileFormat:         fileFormat,
		ParserUsed:         parserUsed,
		DeclaredTotalCents: req.DeclaredTotalCents,
		ReplaceStatementID: req.ReplaceStatementID,
		Transactions:       txs,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}

	if stagingID != "" {
		relPath, err := s.staging.PromoteToUploads(stagingID, s.uploadsDir, stmt.ReferenceYear, stmt.ReferenceMonth, stmt.ID)
		if err != nil {
			_, _ = s.store.DeleteStatement(r.Context(), stmt.ID)
			writeError(w, http.StatusInternalServerError, "falha ao promover arquivo para armazenamento")
			return
		}
		if err := s.store.UpdateStatementStoredPath(r.Context(), stmt.ID, relPath); err != nil {
			s.removeUploadedFile(relPath)
			_, _ = s.store.DeleteStatement(r.Context(), stmt.ID)
			writeStoreError(w, err)
			return
		}
		stmt.StoredPath = relPath
	}

	if oldStoredPath != "" {
		s.removeUploadedFile(oldStoredPath)
	}

	writeJSON(w, http.StatusCreated, stmt)
}

func (s *Server) buildPreviewResult(ctx context.Context, meta parser.StagingMeta, outcome *parser.ParseOutcome) (domain.PreviewResult, error) {
	txs := make([]domain.PreviewTransaction, 0, len(outcome.Transactions))
	for _, tx := range outcome.Transactions {
		if tx.Splits == nil {
			tx.Splits = make([]domain.SplitInput, 0)
		}
		if tx.SuggestedPersonID == nil && strings.TrimSpace(tx.NormalizedMerchant) != "" {
			suggestedID, err := s.store.LookupSuggestedPerson(ctx, tx.NormalizedMerchant)
			if err != nil {
				return domain.PreviewResult{}, err
			}
			tx.SuggestedPersonID = suggestedID
		}
		txs = append(txs, tx)
	}

	dupWarning, err := s.store.FindDuplicates(ctx, meta.FileSHA256, outcome.Bank, outcome.ReferenceYear, outcome.ReferenceMonth)
	if err != nil {
		return domain.PreviewResult{}, err
	}

	return domain.PreviewResult{
		Status:             outcome.Status,
		StagingID:          meta.ID,
		OriginalFilename:   meta.OriginalFilename,
		FileFormat:         outcome.FileFormat,
		ParserUsed:         outcome.ParserUsed,
		Bank:               outcome.Bank,
		ReferenceYear:      outcome.ReferenceYear,
		ReferenceMonth:     outcome.ReferenceMonth,
		DeclaredTotalCents: outcome.DeclaredTotalCents,
		ComputedTotalCents: outcome.ComputedTotalCents,
		Transactions:       txs,
		DuplicateWarning:   dupWarning,
		Reason:             outcome.Reason,
		HasExtractableText: outcome.HasExtractableText,
		TextPreview:        outcome.TextPreview,
	}, nil
}

func detectFormatFromFilename(filename string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	switch ext {
	case "pdf", "csv", "ofx":
		return ext
	case "qfx":
		return "ofx"
	case "":
		return "unknown"
	default:
		return ext
	}
}

func truncatePreviewText(text string) string {
	trimmed := strings.TrimSpace(text)
	runes := []rune(trimmed)
	if len(runes) > 500 {
		return string(runes[:500])
	}
	return trimmed
}
