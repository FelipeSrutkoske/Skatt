package receipt

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	FromName string
	TLSMode  string
}

type Mailer interface {
	SendEmail(ctx context.Context, cfg SMTPConfig, to, subject, htmlBody string) error
}

type SMTPMailer struct{}

func NewSMTPMailer() *SMTPMailer {
	return &SMTPMailer{}
}

func (m *SMTPMailer) SendEmail(ctx context.Context, cfg SMTPConfig, to, subject, htmlBody string) error {
	return SendEmail(ctx, cfg, to, subject, htmlBody)
}

func containsHeaderInjection(s string) bool {
	return strings.ContainsAny(s, "\r\n")
}

func normalizeTLSMode(raw string) string {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		return "starttls"
	}
	return mode
}

func ValidateSMTPConfig(cfg SMTPConfig, to string) error {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return fmt.Errorf("servidor SMTP (host) nao informado")
	}
	if containsHeaderInjection(host) {
		return fmt.Errorf("servidor SMTP contem caracteres invalidos")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("porta SMTP invalida: %d", cfg.Port)
	}

	tlsMode := normalizeTLSMode(cfg.TLSMode)
	if tlsMode != "starttls" && tlsMode != "tls" && tlsMode != "none" {
		return fmt.Errorf("modo TLS SMTP invalido: %q", cfg.TLSMode)
	}

	user := strings.TrimSpace(cfg.Username)
	if user == "" {
		return fmt.Errorf("usuario/remetente SMTP nao informado")
	}
	if containsHeaderInjection(user) {
		return fmt.Errorf("usuario SMTP contem caracteres invalidos")
	}
	if _, err := mail.ParseAddress(user); err != nil {
		return fmt.Errorf("remetente SMTP invalido: %w", err)
	}

	if containsHeaderInjection(cfg.FromName) {
		return fmt.Errorf("nome do remetente contem quebra de linha invalida")
	}

	recipient := strings.TrimSpace(to)
	if recipient == "" {
		return fmt.Errorf("destinatario de e-mail nao informado")
	}
	if containsHeaderInjection(recipient) {
		return fmt.Errorf("destinatario contem quebra de linha invalida")
	}
	if _, err := mail.ParseAddress(recipient); err != nil {
		return fmt.Errorf("destinatario de e-mail invalido: %w", err)
	}

	return nil
}

func buildMIMEMessage(fromAddr, fromName, toAddr, subject, htmlBody string) ([]byte, error) {
	if containsHeaderInjection(subject) {
		return nil, fmt.Errorf("assunto contem quebra de linha invalida")
	}
	fromHeader := (&mail.Address{
		Name:    strings.TrimSpace(fromName),
		Address: fromAddr,
	}).String()

	encodedSubject := mime.QEncoding.Encode("utf-8", strings.TrimSpace(subject))

	var buf bytes.Buffer
	buf.WriteString("From: ")
	buf.WriteString(fromHeader)
	buf.WriteString("\r\n")
	buf.WriteString("To: ")
	buf.WriteString(toAddr)
	buf.WriteString("\r\n")
	buf.WriteString("Subject: ")
	buf.WriteString(encodedSubject)
	buf.WriteString("\r\n")
	buf.WriteString("Date: ")
	buf.WriteString(time.Now().Format(time.RFC1123Z))
	buf.WriteString("\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	buf.WriteString("\r\n")

	qp := quotedprintable.NewWriter(&buf)
	if _, err := qp.Write([]byte(htmlBody)); err != nil {
		return nil, fmt.Errorf("falha ao codificar corpo HTML: %w", err)
	}
	if err := qp.Close(); err != nil {
		return nil, fmt.Errorf("falha ao finalizar codificacao HTML: %w", err)
	}

	return buf.Bytes(), nil
}

func SendEmail(ctx context.Context, cfg SMTPConfig, to, subject, htmlBody string) error {
	if err := ValidateSMTPConfig(cfg, to); err != nil {
		return err
	}
	if containsHeaderInjection(subject) {
		return fmt.Errorf("assunto contem quebra de linha invalida")
	}

	fromParsed, err := mail.ParseAddress(strings.TrimSpace(cfg.Username))
	if err != nil {
		return fmt.Errorf("remetente SMTP invalido: %w", err)
	}
	toParsed, err := mail.ParseAddress(strings.TrimSpace(to))
	if err != nil {
		return fmt.Errorf("destinatario de e-mail invalido: %w", err)
	}

	msg, err := buildMIMEMessage(fromParsed.Address, cfg.FromName, toParsed.Address, subject, htmlBody)
	if err != nil {
		return err
	}

	host := strings.TrimSpace(cfg.Host)
	addr := net.JoinHostPort(host, strconv.Itoa(cfg.Port))
	tlsMode := normalizeTLSMode(cfg.TLSMode)
	tlsCfg := &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	}

	dialer := &net.Dialer{Timeout: 15 * time.Second}

	var conn net.Conn
	if tlsMode == "tls" {
		tlsDialer := &tls.Dialer{
			NetDialer: dialer,
			Config:    tlsCfg,
		}
		conn, err = tlsDialer.DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("falha ao conectar no servidor SMTP: %w", err)
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("falha ao iniciar sessao SMTP: %w", err)
	}
	defer func() {
		_ = client.Close()
	}()

	if tlsMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("servidor SMTP nao suporta STARTTLS")
		}
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("falha ao negociar STARTTLS: %w", err)
		}
	}

	if cfg.Password != "" {
		auth := smtp.PlainAuth("", strings.TrimSpace(cfg.Username), cfg.Password, host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("falha na autenticacao SMTP: %w", err)
		}
	}

	if err := client.Mail(fromParsed.Address); err != nil {
		return fmt.Errorf("falha ao definir remetente SMTP: %w", err)
	}
	if err := client.Rcpt(toParsed.Address); err != nil {
		return fmt.Errorf("falha ao definir destinatario SMTP: %w", err)
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("falha ao iniciar envio de dados SMTP: %w", err)
	}
	if _, err := writer.Write(msg); err != nil {
		_ = writer.Close()
		return fmt.Errorf("falha ao gravar corpo do e-mail: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("falha ao concluir envio de dados SMTP: %w", err)
	}

	if err := client.Quit(); err != nil {
		return fmt.Errorf("falha ao encerrar sessao SMTP: %w", err)
	}
	return nil
}
