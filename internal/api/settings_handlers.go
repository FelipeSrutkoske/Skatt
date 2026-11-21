package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/FelipeSrutkoske/Skatt/internal/receipt"
)

type updateSettingsRequest struct {
	SMTPHost           *string `json:"smtp_host"`
	SMTPPort           *int    `json:"smtp_port"`
	SMTPUser           *string `json:"smtp_user"`
	SMTPPass           *string `json:"smtp_pass"`
	ClearSMTPPass      bool    `json:"clear_smtp_pass"`
	SMTPFromName       *string `json:"smtp_from_name"`
	SMTPTLSMode        *string `json:"smtp_tls_mode"`
	AIProvider         *string `json:"ai_provider"`
	AIModel            *string `json:"ai_model"`
	AIAPIKey           *string `json:"ai_api_key"`
	ClearAIAPIKey      bool    `json:"clear_ai_api_key"`
	BackupReminderDays *int    `json:"backup_reminder_days"`
}

type smtpTestRequest struct {
	To string `json:"to"`
}

type operationResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	view, err := s.store.GetSettingsView(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req updateSettingsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	kv := make(map[string]string)

	if req.SMTPHost != nil {
		kv["smtp_host"] = strings.TrimSpace(*req.SMTPHost)
	}
	if req.SMTPPort != nil {
		if *req.SMTPPort < 1 || *req.SMTPPort > 65535 {
			writeError(w, http.StatusBadRequest, "smtp_port deve estar entre 1 e 65535")
			return
		}
		kv["smtp_port"] = strconv.Itoa(*req.SMTPPort)
	}
	if req.SMTPUser != nil {
		kv["smtp_user"] = strings.TrimSpace(*req.SMTPUser)
	}
	if req.ClearSMTPPass {
		kv["smtp_pass"] = ""
	} else if req.SMTPPass != nil && *req.SMTPPass != "" {
		kv["smtp_pass"] = *req.SMTPPass
	}
	if req.SMTPFromName != nil {
		kv["smtp_from_name"] = strings.TrimSpace(*req.SMTPFromName)
	}
	if req.SMTPTLSMode != nil {
		mode := strings.ToLower(strings.TrimSpace(*req.SMTPTLSMode))
		if mode == "" {
			mode = "starttls"
		}
		if mode != "starttls" && mode != "tls" && mode != "none" {
			writeError(w, http.StatusBadRequest, "smtp_tls_mode invalido")
			return
		}
		kv["smtp_tls_mode"] = mode
	}
	if req.AIProvider != nil {
		kv["ai_provider"] = strings.TrimSpace(*req.AIProvider)
	}
	if req.AIModel != nil {
		kv["ai_model"] = strings.TrimSpace(*req.AIModel)
	}
	if req.ClearAIAPIKey {
		kv["ai_api_key"] = ""
	} else if req.AIAPIKey != nil && strings.TrimSpace(*req.AIAPIKey) != "" {
		kv["ai_api_key"] = strings.TrimSpace(*req.AIAPIKey)
	}
	if req.BackupReminderDays != nil {
		if *req.BackupReminderDays <= 0 {
			writeError(w, http.StatusBadRequest, "backup_reminder_days deve ser maior que zero")
			return
		}
		kv["backup_reminder_days"] = strconv.Itoa(*req.BackupReminderDays)
	}

	if len(kv) > 0 {
		if err := s.store.SetSettings(r.Context(), kv); err != nil {
			writeStoreError(w, err)
			return
		}
	}

	view, err := s.store.GetSettingsView(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleTestSMTP(w http.ResponseWriter, r *http.Request) {
	var req smtpTestRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	to := strings.TrimSpace(req.To)
	if to == "" {
		writeError(w, http.StatusBadRequest, "destinatario to obrigatorio")
		return
	}

	cfg, err := s.loadSMTPConfig(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}

	if err := receipt.ValidateSMTPConfig(cfg, to); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	subject := "Teste de configuracao SMTP - Finance"
	body := "<!DOCTYPE html><html lang=\"pt-BR\"><body><p>Configuracao SMTP validada com sucesso.</p></body></html>"
	if err := s.mailer.SendEmail(r.Context(), cfg, to, subject, body); err != nil {
		errMsg := err.Error()
		if cfg.Password != "" {
			errMsg = strings.ReplaceAll(errMsg, cfg.Password, "[REDACTED]")
		}
		writeError(w, http.StatusBadGateway, errMsg)
		return
	}

	writeJSON(w, http.StatusOK, operationResult{
		OK:      true,
		Message: "E-mail de teste enviado com sucesso.",
	})
}

func (s *Server) loadSMTPConfig(ctx context.Context) (receipt.SMTPConfig, error) {
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return receipt.SMTPConfig{}, err
	}

	port := 587
	if rawPort := strings.TrimSpace(settings["smtp_port"]); rawPort != "" {
		if parsed, err := strconv.Atoi(rawPort); err == nil && parsed > 0 {
			port = parsed
		}
	}

	tlsMode := strings.TrimSpace(settings["smtp_tls_mode"])
	if tlsMode == "" {
		tlsMode = "starttls"
	}

	return receipt.SMTPConfig{
		Host:     strings.TrimSpace(settings["smtp_host"]),
		Port:     port,
		Username: strings.TrimSpace(settings["smtp_user"]),
		Password: settings["smtp_pass"],
		FromName: strings.TrimSpace(settings["smtp_from_name"]),
		TLSMode:  tlsMode,
	}, nil
}
