package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
)

func (s *Store) GetSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.DB().QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("listar configuracoes: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("ler configuracao: %w", err)
		}
		out[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar configuracoes: %w", err)
	}
	return out, nil
}

func (s *Store) SetSettings(ctx context.Context, kv map[string]string) error {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("iniciar transacao salvar configuracoes: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for k, v := range kv {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		if err := upsertSettingExec(ctx, tx, key, v); err != nil {
			return fmt.Errorf("salvar configuracao %s: %w", key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("confirmar configuracoes: %w", err)
	}
	return nil
}

func (s *Store) TouchMutation(ctx context.Context) error {
	if err := touchMutationExec(ctx, s.DB()); err != nil {
		return fmt.Errorf("registrar mutacao: %w", err)
	}
	return nil
}

func (s *Store) TouchBackup(ctx context.Context) error {
	if err := upsertSettingExec(ctx, s.DB(), "last_backup_at", nowTimestamp()); err != nil {
		return fmt.Errorf("registrar backup: %w", err)
	}
	return nil
}

func (s *Store) GetSettingsView(ctx context.Context) (domain.SettingsView, error) {
	m, err := s.GetSettings(ctx)
	if err != nil {
		return domain.SettingsView{}, err
	}

	smtpPort := 587
	if rawPort := strings.TrimSpace(m["smtp_port"]); rawPort != "" {
		if p, err := strconv.Atoi(rawPort); err == nil && p > 0 {
			smtpPort = p
		}
	}

	tlsMode := strings.TrimSpace(m["smtp_tls_mode"])
	if tlsMode == "" {
		tlsMode = "starttls"
	}

	aiProvider := strings.TrimSpace(m["ai_provider"])
	if aiProvider == "" {
		aiProvider = "gemini"
	}

	aiModel := strings.TrimSpace(m["ai_model"])
	if aiModel == "" {
		aiModel = "gemini-2.5-flash"
	}

	reminderDays := 14
	if rawDays := strings.TrimSpace(m["backup_reminder_days"]); rawDays != "" {
		if d, err := strconv.Atoi(rawDays); err == nil && d > 0 {
			reminderDays = d
		}
	}

	return domain.SettingsView{
		SMTPHost:           m["smtp_host"],
		SMTPPort:           smtpPort,
		SMTPUser:           m["smtp_user"],
		SMTPFromName:       m["smtp_from_name"],
		SMTPTLSMode:        tlsMode,
		HasSMTPPass:        strings.TrimSpace(m["smtp_pass"]) != "",
		AIProvider:         aiProvider,
		AIModel:            aiModel,
		HasAIAPIKey:        strings.TrimSpace(m["ai_api_key"]) != "",
		BackupReminderDays: reminderDays,
		LastBackupAt:       m["last_backup_at"],
		LastMutationAt:     m["last_data_mutation_at"],
	}, nil
}
