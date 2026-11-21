package receipt

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"unicode"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
)

func intPtr(v int) *int {
	return &v
}

func containsEmoji(s string) bool {
	for _, r := range s {
		if (r >= 0x1F300 && r <= 0x1FAFF) || (r >= 0x2600 && r <= 0x27BF) {
			return true
		}
		if unicode.Is(unicode.So, r) {
			return true
		}
	}
	return false
}

func TestRenderHTMLSingleBankOmitsPaymentsAndPriorBalance(t *testing.T) {
	data := domain.ReceiptPreview{
		PersonID:            2,
		PersonName:          "Regina <script>alert(1)</script>",
		PersonEmail:         "regina@example.com",
		ReferenceYear:       2025,
		ReferenceMonth:      4,
		BankFilter:          "nubank",
		IncludePriorBalance: true,
		Items: []domain.ReceiptItem{
			{
				PurchaseDate:          "2025-04-10",
				Bank:                  "nubank",
				Description:           "LOJA <img src=x onerror=alert(1)>",
				InstallmentCurrent:    intPtr(2),
				InstallmentTotal:      intPtr(6),
				TransactionTotalCents: 12000,
				PersonAmountCents:     7000,
				IsSplit:               true,
			},
		},
		SubtotalCents:     7000,
		PaidCents:         5000,
		PriorBalanceCents: 3000,
		TotalCents:        7000,
	}

	html, err := RenderHTML(data)
	if err != nil {
		t.Fatalf("RenderHTML falhou: %v", err)
	}

	if strings.Contains(html, "<script>") || strings.Contains(html, "<img src=x") {
		t.Fatalf("HTML nao escapou conteudo potencialmente malicioso")
	}
	if !strings.Contains(html, "&lt;script&gt;") || !strings.Contains(html, "&lt;img") {
		t.Fatalf("conteudo escapado esperado nao encontrado")
	}
	if strings.Contains(html, "Pagamentos abatidos") {
		t.Fatalf("recibo de banco unico nao pode exibir Pagamentos abatidos")
	}
	if strings.Contains(html, "Saldo anterior") {
		t.Fatalf("recibo de banco unico nao pode exibir Saldo anterior")
	}
	if !strings.Contains(html, "Subtotal (Nubank)") || !strings.Contains(html, "R$ 70,00") {
		t.Fatalf("subtotal do banco esperado nao encontrado")
	}
	if !strings.Contains(html, "Parcela 02/06") || !strings.Contains(html, "Parte de R$ 120,00") {
		t.Fatalf("detalhes de parcela e divisao nao encontrados")
	}
	if !strings.Contains(html, "@media print") || !strings.Contains(html, "tabular-nums") {
		t.Fatalf("CSS de impressao ou tabular-nums ausente")
	}
	if containsEmoji(html) {
		t.Fatalf("HTML nao deve conter emotes")
	}
}

func TestRenderHTMLConsolidatedShowsPaymentsAndOptionalPriorBalance(t *testing.T) {
	base := domain.ReceiptPreview{
		PersonID:       2,
		PersonName:     "Regina",
		ReferenceYear:  2025,
		ReferenceMonth: 4,
		BankFilter:     "",
		Items: []domain.ReceiptItem{
			{
				PurchaseDate:          "2025-04-10",
				Bank:                  "nubank",
				Description:           "UBER *TRIP",
				TransactionTotalCents: 1990,
				PersonAmountCents:     1990,
			},
			{
				PurchaseDate:          "2025-04-12",
				Bank:                  "itau",
				CardLabel:             "Final 4321",
				Description:           "MERCADO",
				TransactionTotalCents: 10000,
				PersonAmountCents:     10000,
			},
		},
		SubtotalCents:     11990,
		PaidCents:         5000,
		PriorBalanceCents: 3000,
		TotalCents:        6990,
	}

	withoutPrior, err := RenderHTML(base)
	if err != nil {
		t.Fatalf("RenderHTML sem saldo anterior falhou: %v", err)
	}
	if !strings.Contains(withoutPrior, "Subtotal do período") || !strings.Contains(withoutPrior, "R$ 119,90") {
		t.Fatalf("subtotal consolidado ausente")
	}
	if !strings.Contains(withoutPrior, "Pagamentos abatidos") || !strings.Contains(withoutPrior, "-R$ 50,00") {
		t.Fatalf("pagamentos abatidos ausente")
	}
	if strings.Contains(withoutPrior, "Saldo anterior") {
		t.Fatalf("Saldo anterior nao deve aparecer quando IncludePriorBalance == false")
	}
	if !strings.Contains(withoutPrior, "Saldo final") || !strings.Contains(withoutPrior, "R$ 69,90") {
		t.Fatalf("saldo final ausente")
	}
	if !strings.Contains(withoutPrior, "Itaú (Final 4321)") {
		t.Fatalf("nome do banco e rotulo do cartao ausentes")
	}

	withPriorData := base
	withPriorData.IncludePriorBalance = true
	withPriorData.TotalCents = 9990

	withPrior, err := RenderHTML(withPriorData)
	if err != nil {
		t.Fatalf("RenderHTML com saldo anterior falhou: %v", err)
	}
	if !strings.Contains(withPrior, "Saldo anterior") || !strings.Contains(withPrior, "R$ 30,00") {
		t.Fatalf("Saldo anterior esperado quando IncludePriorBalance == true")
	}
	if !strings.Contains(withPrior, "R$ 99,90") {
		t.Fatalf("saldo final com saldo anterior ausente")
	}
}

func TestBuildSubjectAndRenderWhatsAppTextFormat(t *testing.T) {
	single := domain.ReceiptPreview{
		PersonName:     "Regina",
		ReferenceYear:  2025,
		ReferenceMonth: 4,
		BankFilter:     "mercadopago",
		Items: []domain.ReceiptItem{
			{
				PurchaseDate:          "2025-04-08",
				Bank:                  "mercadopago",
				Description:           "FARMACIA",
				TransactionTotalCents: 4590,
				PersonAmountCents:     4590,
			},
		},
		SubtotalCents:     4590,
		PaidCents:         1000,
		PriorBalanceCents: 2000,
		TotalCents:        4590,
	}

	subjSingle := BuildSubject(single)
	if subjSingle != "Resumo de fatura - Mercado Pago - Abril/2025 - Regina" {
		t.Fatalf("assunto banco unico inesperado: %q", subjSingle)
	}

	waSingle := RenderWhatsAppText(single)
	if containsEmoji(waSingle) {
		t.Fatalf("texto WhatsApp nao pode conter emotes: %s", waSingle)
	}
	if strings.Contains(waSingle, "Pagamentos abatidos") || strings.Contains(waSingle, "Saldo anterior") {
		t.Fatalf("WhatsApp de banco unico nao pode conter pagamentos ou saldo anterior: %s", waSingle)
	}
	if !strings.Contains(waSingle, "08/04/2025 | FARMACIA | R$ 45,90") || !strings.Contains(waSingle, "Subtotal (Mercado Pago): R$ 45,90") {
		t.Fatalf("formato WhatsApp banco unico inesperado: %s", waSingle)
	}

	consolidated := domain.ReceiptPreview{
		PersonName:          "Regina",
		ReferenceYear:       2025,
		ReferenceMonth:      4,
		BankFilter:          "",
		IncludePriorBalance: true,
		Items: []domain.ReceiptItem{
			{
				PurchaseDate:          "2025-04-12",
				Bank:                  "nubank",
				Description:           "AMAZON",
				InstallmentCurrent:    intPtr(1),
				InstallmentTotal:      intPtr(3),
				TransactionTotalCents: 12000,
				PersonAmountCents:     7000,
				IsSplit:               true,
			},
		},
		SubtotalCents:     7000,
		PaidCents:         2000,
		PriorBalanceCents: 1500,
		TotalCents:        6500,
	}

	subjConsolidated := BuildSubject(consolidated)
	if subjConsolidated != "Resumo de faturas - Abril/2025 - Regina" {
		t.Fatalf("assunto consolidado inesperado: %q", subjConsolidated)
	}

	waConsolidated := RenderWhatsAppText(consolidated)
	if containsEmoji(waConsolidated) {
		t.Fatalf("texto WhatsApp consolidado nao pode conter emotes: %s", waConsolidated)
	}
	for _, want := range []string{
		"12/04/2025 | Nubank | AMAZON (Parcela 01/03) [Parte de R$ 120,00] | R$ 70,00",
		"Subtotal do período: R$ 70,00",
		"Pagamentos abatidos: -R$ 20,00",
		"Saldo anterior: R$ 15,00",
		"Saldo final: R$ 65,00",
	} {
		if !strings.Contains(waConsolidated, want) {
			t.Fatalf("esperava %q no texto WhatsApp:\n%s", want, waConsolidated)
		}
	}
}

func TestSMTPConfigValidationAndHeaderInjection(t *testing.T) {
	validCfg := SMTPConfig{
		Host:     "smtp.example.com",
		Port:     587,
		Username: "sender@example.com",
		Password: "secret",
		FromName: "Felipe",
		TLSMode:  "starttls",
	}

	cases := []struct {
		name    string
		cfg     SMTPConfig
		to      string
		subject string
	}{
		{
			name:    "host vazio",
			cfg:     SMTPConfig{Port: 587, Username: "a@b.com", TLSMode: "starttls"},
			to:      "dest@example.com",
			subject: "Assunto",
		},
		{
			name:    "porta invalida",
			cfg:     SMTPConfig{Host: "smtp.example.com", Port: 0, Username: "a@b.com", TLSMode: "starttls"},
			to:      "dest@example.com",
			subject: "Assunto",
		},
		{
			name:    "modo tls invalido",
			cfg:     SMTPConfig{Host: "smtp.example.com", Port: 587, Username: "a@b.com", TLSMode: "invalid"},
			to:      "dest@example.com",
			subject: "Assunto",
		},
		{
			name:    "destinatario invalido",
			cfg:     validCfg,
			to:      "invalido",
			subject: "Assunto",
		},
		{
			name:    "header injection no assunto",
			cfg:     validCfg,
			to:      "dest@example.com",
			subject: "Resumo\r\nBcc: attacker@example.com",
		},
		{
			name:    "header injection no destinatario",
			cfg:     validCfg,
			to:      "dest@example.com\r\nBcc: attacker@example.com",
			subject: "Resumo",
		},
		{
			name:    "header injection no remetente",
			cfg:     SMTPConfig{Host: "smtp.example.com", Port: 587, Username: "a@b.com", FromName: "Nome\nEvil: 1", TLSMode: "starttls"},
			to:      "dest@example.com",
			subject: "Resumo",
		},
	}

	mailer := NewSMTPMailer()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := mailer.SendEmail(context.Background(), tc.cfg, tc.to, tc.subject, "<p>oi</p>"); err == nil {
				t.Fatalf("esperava erro para caso %q", tc.name)
			}
		})
	}
}

func TestSendEmailPlainLocalMockSMTP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("falha ao abrir listener local: %v", err)
	}
	defer ln.Close()

	receivedCh := make(chan string, 1)
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		writeLine := func(line string) {
			_, _ = fmt.Fprintf(conn, "%s\r\n", line)
		}
		writeLine("220 localhost ESMTP")

		var dataBuf strings.Builder
		inData := false
		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				break
			}
			trimmed := strings.TrimRight(line, "\r\n")
			if inData {
				if trimmed == "." {
					inData = false
					writeLine("250 OK")
					continue
				}
				dataBuf.WriteString(line)
				continue
			}
			upper := strings.ToUpper(trimmed)
			switch {
			case strings.HasPrefix(upper, "EHLO") || strings.HasPrefix(upper, "HELO"):
				writeLine("250-localhost")
				writeLine("250 OK")
			case strings.HasPrefix(upper, "MAIL FROM:"):
				writeLine("250 OK")
			case strings.HasPrefix(upper, "RCPT TO:"):
				writeLine("250 OK")
			case upper == "DATA":
				inData = true
				writeLine("354 End data with <CR><LF>.<CR><LF>")
			case upper == "QUIT":
				writeLine("221 Bye")
				receivedCh <- dataBuf.String()
				return
			default:
				writeLine("250 OK")
			}
		}
	}()

	tcpAddr := ln.Addr().(*net.TCPAddr)
	cfg := SMTPConfig{
		Host:     "127.0.0.1",
		Port:     tcpAddr.Port,
		Username: "finance@example.com",
		FromName: "Controle Financeiro",
		TLSMode:  "none",
	}

	err = SendEmail(context.Background(), cfg, "regina@example.com", "Resumo de faturas - Março/2025", "<html><body>Teste</body></html>")
	if err != nil {
		t.Fatalf("SendEmail falhou contra servidor SMTP local: %v", err)
	}

	rawMsg := <-receivedCh
	if !strings.Contains(rawMsg, "Content-Type: text/html; charset=UTF-8") {
		t.Fatalf("header Content-Type ausente na mensagem:\n%s", rawMsg)
	}
	if !strings.Contains(rawMsg, "To: regina@example.com") {
		t.Fatalf("header To ausente na mensagem:\n%s", rawMsg)
	}
	if !strings.Contains(rawMsg, "<html><body>Teste</body></html>") {
		t.Fatalf("corpo HTML ausente na mensagem:\n%s", rawMsg)
	}
}
