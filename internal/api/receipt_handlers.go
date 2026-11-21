package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
	"github.com/FelipeSrutkoske/Skatt/internal/receipt"
	"github.com/FelipeSrutkoske/Skatt/internal/store"
)

func (s *Server) handleGetReceiptAvailability(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	personID, err := strconv.ParseInt(strings.TrimSpace(q.Get("person_id")), 10, 64)
	if err != nil || personID <= 0 {
		writeError(w, http.StatusBadRequest, "parametro person_id invalido")
		return
	}

	year, err := strconv.Atoi(strings.TrimSpace(q.Get("year")))
	if err != nil || year <= 0 {
		writeError(w, http.StatusBadRequest, "parametro year invalido")
		return
	}

	bank := strings.TrimSpace(q.Get("bank"))

	months, err := s.store.GetReceiptAvailability(r.Context(), personID, year, bank)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, months)
}

type receiptPreviewRequest struct {
	PersonID            int64  `json:"person_id"`
	ReferenceYear       int    `json:"reference_year"`
	ReferenceMonth      int    `json:"reference_month"`
	BankFilter          string `json:"bank_filter"`
	IncludePriorBalance bool   `json:"include_prior_balance"`
}

type sendReceiptRequest struct {
	PersonID            int64  `json:"person_id"`
	ReferenceYear       int    `json:"reference_year"`
	ReferenceMonth      int    `json:"reference_month"`
	BankFilter          string `json:"bank_filter"`
	IncludePriorBalance bool   `json:"include_prior_balance"`
	RecipientEmail      string `json:"recipient_email"`
	Subject             string `json:"subject"`
}

func (s *Server) handlePreviewReceipt(w http.ResponseWriter, r *http.Request) {
	var req receiptPreviewRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	preview, err := s.store.ComputeReceiptData(r.Context(), store.ReceiptQuery{
		PersonID:            req.PersonID,
		ReferenceYear:       req.ReferenceYear,
		ReferenceMonth:      req.ReferenceMonth,
		BankFilter:          req.BankFilter,
		IncludePriorBalance: req.IncludePriorBalance,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}

	preview.Subject = receipt.BuildSubject(preview)
	htmlBody, err := receipt.RenderHTML(preview)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao renderizar HTML do recibo")
		return
	}
	preview.HTMLBody = htmlBody
	preview.WhatsAppText = receipt.RenderWhatsAppText(preview)

	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) handleSendReceipt(w http.ResponseWriter, r *http.Request) {
	var req sendReceiptRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	preview, err := s.store.ComputeReceiptData(r.Context(), store.ReceiptQuery{
		PersonID:            req.PersonID,
		ReferenceYear:       req.ReferenceYear,
		ReferenceMonth:      req.ReferenceMonth,
		BankFilter:          req.BankFilter,
		IncludePriorBalance: req.IncludePriorBalance,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}

	recipientEmail := strings.TrimSpace(req.RecipientEmail)
	if recipientEmail == "" {
		recipientEmail = strings.TrimSpace(preview.PersonEmail)
	}
	if recipientEmail == "" {
		writeError(w, http.StatusBadRequest, "recipient_email obrigatorio")
		return
	}

	subject := strings.TrimSpace(req.Subject)
	if subject == "" {
		subject = receipt.BuildSubject(preview)
	}
	preview.Subject = subject

	htmlBody, err := receipt.RenderHTML(preview)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao renderizar HTML do recibo")
		return
	}

	smtpCfg, err := s.loadSMTPConfig(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}

	rec := domain.Receipt{
		PersonID:            preview.PersonID,
		ReferenceYear:       preview.ReferenceYear,
		ReferenceMonth:      preview.ReferenceMonth,
		BankFilter:          preview.BankFilter,
		IncludePriorBalance: preview.IncludePriorBalance,
		RecipientEmail:      recipientEmail,
		Subject:             subject,
		HTMLBody:            htmlBody,
		SubtotalCents:       preview.SubtotalCents,
		PaidCents:           preview.PaidCents,
		PriorBalanceCents:   preview.PriorBalanceCents,
		TotalCents:          preview.TotalCents,
	}

	if sendErr := s.mailer.SendEmail(r.Context(), smtpCfg, recipientEmail, subject, htmlBody); sendErr != nil {
		rec.DeliveryStatus = "failed"
		errMsg := sendErr.Error()
		if smtpCfg.Password != "" {
			errMsg = strings.ReplaceAll(errMsg, smtpCfg.Password, "[REDACTED]")
		}
		rec.ErrorMessage = errMsg
	} else {
		now := time.Now().UTC().Format(time.RFC3339)
		rec.DeliveryStatus = "sent"
		rec.SentAt = &now
	}

	saved, err := s.store.SaveReceipt(r.Context(), rec)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) handleListReceipts(w http.ResponseWriter, r *http.Request) {
	receipts, err := s.store.ListReceipts(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, receipts)
}

func (s *Server) handleGetReceiptHTML(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	rec, err := s.store.GetReceipt(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(rec.HTMLBody))
}
