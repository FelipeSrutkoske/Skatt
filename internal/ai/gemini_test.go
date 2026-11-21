package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiExtractParsesStructuredJSON(t *testing.T) {
	var capturedPath string
	var capturedAPIKey string
	var capturedBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAPIKey = r.Header.Get("x-goog-api-key")
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("erro ao ler corpo da requisicao: %v", err)
		}
		capturedBody = string(raw)

		var reqPayload geminiRequest
		if err := json.Unmarshal(raw, &reqPayload); err != nil {
			t.Fatalf("payload da requisicao nao e JSON valido: %v", err)
		}
		if reqPayload.GenerationConfig.ResponseMimeType != "application/json" {
			t.Fatalf("responseMimeType esperado application/json, obtido %q", reqPayload.GenerationConfig.ResponseMimeType)
		}

		structured := `{
			"bank": "Nubank",
			"reference_year": 2025,
			"reference_month": 4,
			"declared_total_cents": 15980,
			"transactions": [
				{
					"purchase_date": "2025-04-10",
					"description": "UBER *TRIP",
					"amount_cents": 1990,
					"is_ignored": false
				},
				{
					"purchase_date": "10/04/2025",
					"description": "UBER *TRIP",
					"amount_cents": 1990,
					"is_ignored": false
				},
				{
					"purchase_date": "2025-04-12",
					"description": "MAGAZINE LUIZA 02/06",
					"amount_cents": 12000,
					"is_ignored": false
				},
				{
					"purchase_date": "2025-04-15",
					"description": "Pagamento recebido",
					"amount_cents": -15980,
					"is_ignored": true
				}
			]
		}`

		resp := map[string]any{
			"candidates": []any{
				map[string]any{
					"content": map[string]any{
						"parts": []any{
							map[string]any{"text": structured},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewGeminiClient("test-secret-key", "gemini-2.5-flash", server.URL)
	inputText := "Fatura Nubank\nTitular CPF 123.456.789-00\nCartao 4532 1234 5678 9012\nEndereco: Rua Paulista, 1000 - 01310-100\n10/04 UBER *TRIP R$ 19,90\n12/04 MAGAZINE LUIZA 02/06 R$ 120,00"

	res, err := client.ExtractFromText(context.Background(), inputText, "fatura-abril.pdf")
	if err != nil {
		t.Fatalf("ExtractFromText falhou: %v", err)
	}

	if capturedPath != "/v1beta/models/gemini-2.5-flash:generateContent" {
		t.Fatalf("path inesperado: %q", capturedPath)
	}
	if capturedAPIKey != "test-secret-key" {
		t.Fatalf("header x-goog-api-key inesperado: %q", capturedAPIKey)
	}
	if strings.Contains(capturedBody, "123.456.789-00") {
		t.Fatalf("CPF original vazou no corpo da requisicao")
	}
	if strings.Contains(capturedBody, "4532 1234 5678 9012") {
		t.Fatalf("numero de cartao original vazou no corpo da requisicao")
	}
	if strings.Contains(capturedBody, "01310-100") {
		t.Fatalf("CEP original vazou no corpo da requisicao")
	}
	if !strings.Contains(capturedBody, "[CPF]") || !strings.Contains(capturedBody, "[CARTAO]") {
		t.Fatalf("tokens de mascaramento nao encontrados no payload enviado")
	}
	if !strings.Contains(capturedBody, "R$ 19,90") || !strings.Contains(capturedBody, "MAGAZINE LUIZA 02/06") {
		t.Fatalf("valores e descricoes de compras nao foram preservados")
	}

	if res.Bank != "nubank" {
		t.Fatalf("banco esperado nubank, obtido %q", res.Bank)
	}
	if res.ReferenceYear != 2025 || res.ReferenceMonth != 4 {
		t.Fatalf("periodo esperado 2025/4, obtido %d/%d", res.ReferenceYear, res.ReferenceMonth)
	}
	if res.DeclaredTotalCents == nil || *res.DeclaredTotalCents != 15980 {
		t.Fatalf("declared_total_cents esperado 15980, obtido %v", res.DeclaredTotalCents)
	}
	if len(res.Transactions) != 4 {
		t.Fatalf("esperadas 4 transacoes, obtidas %d", len(res.Transactions))
	}

	for i, tx := range res.Transactions {
		if tx.LineIndex != i {
			t.Fatalf("transacao %d esperava LineIndex %d, obtido %d", i, i, tx.LineIndex)
		}
	}

	if res.Transactions[1].PurchaseDate != "2025-04-10" {
		t.Fatalf("data BR nao convertida para ISO: %q", res.Transactions[1].PurchaseDate)
	}
	if res.Transactions[2].NormalizedMerchant != "MAGAZINE LUIZA" {
		t.Fatalf("merchant normalizado esperado MAGAZINE LUIZA, obtido %q", res.Transactions[2].NormalizedMerchant)
	}
	if res.Transactions[2].InstallmentCurrent == nil || *res.Transactions[2].InstallmentCurrent != 2 || res.Transactions[2].InstallmentTotal == nil || *res.Transactions[2].InstallmentTotal != 6 {
		t.Fatalf("parcela esperada 2/6, obtida %v/%v", res.Transactions[2].InstallmentCurrent, res.Transactions[2].InstallmentTotal)
	}
	if !res.Transactions[3].IsIgnored || res.Transactions[3].AmountCents != -15980 {
		t.Fatalf("pagamento recebido esperado ignorado com -15980, obtido ignored=%v amount=%d", res.Transactions[3].IsIgnored, res.Transactions[3].AmountCents)
	}
}

func TestGeminiExtractRejectsEmptyAPIKeyOrEmptyText(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	clientNoKey := NewGeminiClient("   ", "", server.URL)
	if _, err := clientNoKey.ExtractFromText(context.Background(), "texto da fatura", "fatura.pdf"); err == nil {
		t.Fatalf("esperava erro para apiKey vazia")
	}
	if called {
		t.Fatalf("nao deveria realizar requisicao HTTP com apiKey vazia")
	}

	clientNoText := NewGeminiClient("valid-key", "", server.URL)
	if _, err := clientNoText.ExtractFromText(context.Background(), "   \n\t  ", "fatura.pdf"); err == nil {
		t.Fatalf("esperava erro para texto vazio")
	}
	if called {
		t.Fatalf("nao deveria realizar requisicao HTTP com texto vazio")
	}
}

func TestGeminiExtractHandlesCodeFenceAndHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") == "bad-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid key"}}`))
			return
		}
		resp := map[string]any{
			"candidates": []any{
				map[string]any{
					"content": map[string]any{
						"parts": []any{
							map[string]any{
								"text": "```json\n{\"bank\":\"Mercado Pago\",\"reference_year\":2025,\"reference_month\":5,\"declared_total_cents\":null,\"transactions\":[{\"purchase_date\":\"2025-05-01\",\"description\":\"Mercado Livre Parcela 3 de 10\",\"amount_cents\":\"4500\",\"is_ignored\":false}]}\n```",
							},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	badClient := NewGeminiClient("bad-key", "", server.URL)
	if _, err := badClient.ExtractFromText(context.Background(), "01/05 COMPRA R$ 45,00", "mp.pdf"); err == nil {
		t.Fatalf("esperava erro para status 401")
	}

	goodClient := NewGeminiClient("good-key", "", server.URL)
	res, err := goodClient.ExtractFromText(context.Background(), "01/05 Mercado Livre Parcela 3 de 10 R$ 45,00", "mp.pdf")
	if err != nil {
		t.Fatalf("ExtractFromText falhou com code fence: %v", err)
	}
	if res.Bank != "mercadopago" || res.DeclaredTotalCents != nil {
		t.Fatalf("metadados inesperados: bank=%q declared=%v", res.Bank, res.DeclaredTotalCents)
	}
	if len(res.Transactions) != 1 || res.Transactions[0].AmountCents != 4500 {
		t.Fatalf("transacao inesperada: %+v", res.Transactions)
	}
	if res.Transactions[0].NormalizedMerchant != "MERCADO LIVRE" {
		t.Fatalf("merchant esperado MERCADO LIVRE, obtido %q", res.Transactions[0].NormalizedMerchant)
	}
	if res.Transactions[0].InstallmentCurrent == nil || *res.Transactions[0].InstallmentCurrent != 3 || res.Transactions[0].InstallmentTotal == nil || *res.Transactions[0].InstallmentTotal != 10 {
		t.Fatalf("parcela esperada 3/10, obtida %v/%v", res.Transactions[0].InstallmentCurrent, res.Transactions[0].InstallmentTotal)
	}
}

func TestMaskPIIPreservesTransactions(t *testing.T) {
	input := "CPF: 12345678901\nCPF 987.654.321-00\nCartao 4532-1234-5678-9012\nCEP 01310-100\n10/04/2025 FARMACIA SAO PAULO R$ 1.234,56"
	masked := MaskPII(input)
	if strings.Contains(masked, "12345678901") || strings.Contains(masked, "987.654.321-00") {
		t.Fatalf("CPF nao foi mascarado: %s", masked)
	}
	if strings.Contains(masked, "4532-1234-5678-9012") {
		t.Fatalf("cartao nao foi mascarado: %s", masked)
	}
	if strings.Contains(masked, "01310-100") {
		t.Fatalf("CEP nao foi mascarado: %s", masked)
	}
	if !strings.Contains(masked, "10/04/2025 FARMACIA SAO PAULO R$ 1.234,56") {
		t.Fatalf("linha de transacao foi alterada indevidamente: %s", masked)
	}
}
