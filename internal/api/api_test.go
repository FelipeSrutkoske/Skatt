package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/FelipeSrutkoske/Skatt/internal/ai"
	"github.com/FelipeSrutkoske/Skatt/internal/api"
	"github.com/FelipeSrutkoske/Skatt/internal/db"
	"github.com/FelipeSrutkoske/Skatt/internal/domain"
	"github.com/FelipeSrutkoske/Skatt/internal/parser"
	"github.com/FelipeSrutkoske/Skatt/internal/receipt"
	"github.com/FelipeSrutkoske/Skatt/internal/store"
)

type mockMailer struct {
	mu       sync.Mutex
	calls    int
	lastCfg  receipt.SMTPConfig
	lastTo   string
	lastSubj string
	lastHTML string
	failErr  error
}

func (m *mockMailer) SendEmail(_ context.Context, cfg receipt.SMTPConfig, to, subject, htmlBody string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.lastCfg = cfg
	m.lastTo = to
	m.lastSubj = subject
	m.lastHTML = htmlBody
	return m.failErr
}

type mockAIExtractor struct {
	onExtract func(ctx context.Context, maskedText, filename string) (*ai.ExtractResult, error)
}

func (m *mockAIExtractor) ExtractFromText(ctx context.Context, maskedText, filename string) (*ai.ExtractResult, error) {
	return m.onExtract(ctx, maskedText, filename)
}

type testEnv struct {
	server    *httptest.Server
	store     *store.Store
	staging   *parser.StagingManager
	mailer    *mockMailer
	dataDir   string
	aiCalls   int
	aiMu      sync.Mutex
	aiHandler func(apiKey, model, maskedText, filename string) (*ai.ExtractResult, error)
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	uploadsDir := filepath.Join(dataDir, "uploads")
	stagingDir := filepath.Join(dataDir, "staging")

	conn, err := db.Open(filepath.Join(dataDir, "finance.db"))
	if err != nil {
		t.Fatalf("abrir banco: %v", err)
	}
	st := store.New(conn)
	t.Cleanup(func() {
		if active := st.DB(); active != nil {
			_ = active.Close()
		}
	})

	mailer := &mockMailer{}
	env := &testEnv{
		store:   st,
		staging: parser.NewStagingManager(stagingDir),
		mailer:  mailer,
		dataDir: dataDir,
	}

	webFS := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<!doctype html><html><body>SPA</body></html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('ok');")},
	}

	handler := api.NewServer(api.Dependencies{
		Store:      st,
		Staging:    env.staging,
		DataDir:    dataDir,
		UploadsDir: uploadsDir,
		Mailer:     mailer,
		AIExtractorFactory: func(apiKey, model string) ai.Extractor {
			return &mockAIExtractor{
				onExtract: func(_ context.Context, maskedText, filename string) (*ai.ExtractResult, error) {
					env.aiMu.Lock()
					env.aiCalls++
					fn := env.aiHandler
					env.aiMu.Unlock()
					if fn != nil {
						return fn(apiKey, model, maskedText, filename)
					}
					return nil, errors.New("mock ai nao configurado")
				},
			}
		},
		WebFS: webFS,
	})

	env.server = httptest.NewServer(handler)
	t.Cleanup(env.server.Close)
	return env
}

func (e *testEnv) doJSON(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, e.server.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http %s %s: %v", method, path, err)
	}
	return resp
}

func (e *testEnv) uploadFile(t *testing.T, path, fieldName, filename string, content []byte) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, e.server.URL+path, &buf)
	if err != nil {
		t.Fatalf("new multipart request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload %s: %v", path, err)
	}
	return resp
}

func decodeBody[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal body (%s): %v", string(raw), err)
	}
	return out
}

func TestFullImportClassifySplitDashboardReceiptFlow(t *testing.T) {
	env := newTestEnv(t)

	nubankCSV := strings.Join([]string{
		"date,title,amount",
		"2025-04-10,Uber *Trip,19.90",
		"2025-04-10,Uber *Trip,19.90",
		"2025-04-12,Amazon Marketplace,120.00",
		"2025-04-15,Pagamento recebido,-159.80",
	}, "\n")

	prevResp := env.uploadFile(t, "/api/imports/preview", "file", "nubank_2025_04.csv", []byte(nubankCSV))
	if prevResp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, esperado 200", prevResp.StatusCode)
	}
	preview := decodeBody[domain.PreviewResult](t, prevResp)
	if preview.Status != "parsed" {
		t.Fatalf("preview.Status = %q, esperado parsed", preview.Status)
	}
	if preview.Bank != "nubank" || preview.ReferenceYear != 2025 || preview.ReferenceMonth != 4 {
		t.Fatalf("metadados preview inesperados: %+v", preview)
	}
	if len(preview.Transactions) != 4 {
		t.Fatalf("len(transactions) = %d, esperado 4", len(preview.Transactions))
	}
	if preview.ComputedTotalCents != 15980 {
		t.Fatalf("computed_total_cents = %d, esperado 15980", preview.ComputedTotalCents)
	}
	if !preview.Transactions[3].IsIgnored {
		t.Fatalf("Pagamento recebido deveria vir marcado como is_ignored")
	}

	env.aiMu.Lock()
	aiCallsAfterPreview := env.aiCalls
	env.aiMu.Unlock()
	if aiCallsAfterPreview != 0 {
		t.Fatalf("preview nunca deve chamar IA automaticamente; chamadas=%d", aiCallsAfterPreview)
	}

	confirmTxs := make([]map[string]any, 0, len(preview.Transactions))
	for _, tx := range preview.Transactions {
		confirmTxs = append(confirmTxs, map[string]any{
			"purchase_date": tx.PurchaseDate,
			"description":   tx.Description,
			"amount_cents":  tx.AmountCents,
			"is_ignored":    tx.IsIgnored,
		})
	}
	confirmResp := env.doJSON(t, http.MethodPost, "/api/imports/confirm", map[string]any{
		"staging_id":      preview.StagingID,
		"bank":            preview.Bank,
		"card_label":      "Final 1234",
		"reference_year":  preview.ReferenceYear,
		"reference_month": preview.ReferenceMonth,
		"parser_used":     preview.ParserUsed,
		"transactions":    confirmTxs,
	})
	if confirmResp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(confirmResp.Body)
		t.Fatalf("confirm status = %d, body = %s", confirmResp.StatusCode, string(raw))
	}
	stmt := decodeBody[domain.Statement](t, confirmResp)
	if stmt.StoredPath == "" {
		t.Fatalf("stored_path nao deveria ser vazio apos confirmacao com arquivo")
	}

	fileResp, err := http.Get(env.server.URL + "/api/statements/" + itoa64(stmt.ID) + "/file")
	if err != nil {
		t.Fatalf("download file: %v", err)
	}
	fileBytes, _ := io.ReadAll(fileResp.Body)
	fileResp.Body.Close()
	if fileResp.StatusCode != http.StatusOK {
		t.Fatalf("download file status = %d, esperado 200", fileResp.StatusCode)
	}
	if string(fileBytes) != nubankCSV {
		t.Fatalf("conteudo do arquivo baixado difere do original")
	}

	createReginaResp := env.doJSON(t, http.MethodPost, "/api/people", map[string]any{
		"name":  "Regina",
		"email": "regina@example.com",
	})
	if createReginaResp.StatusCode != http.StatusCreated {
		t.Fatalf("create person status = %d, esperado 201", createReginaResp.StatusCode)
	}
	regina := decodeBody[domain.Person](t, createReginaResp)

	peopleResp := env.doJSON(t, http.MethodGet, "/api/people", nil)
	people := decodeBody[[]domain.Person](t, peopleResp)
	var selfID int64
	for _, p := range people {
		if p.IsSelf {
			selfID = p.ID
			break
		}
	}
	if selfID == 0 {
		t.Fatalf("pessoa principal Eu nao encontrada")
	}

	detailResp := env.doJSON(t, http.MethodGet, "/api/statements/"+itoa64(stmt.ID), nil)
	if detailResp.StatusCode != http.StatusOK {
		t.Fatalf("get statement status = %d, esperado 200", detailResp.StatusCode)
	}
	detail := decodeBody[domain.StatementDetail](t, detailResp)
	if len(detail.Transactions) != 4 {
		t.Fatalf("transacoes na fatura = %d, esperado 4", len(detail.Transactions))
	}

	uber1ID := detail.Transactions[0].ID
	amazonID := detail.Transactions[2].ID

	badSplitResp := env.doJSON(t, http.MethodPut, "/api/transactions/"+itoa64(amazonID)+"/splits", map[string]any{
		"splits": []domain.SplitInput{
			{PersonID: selfID, AmountCents: 5000},
			{PersonID: regina.ID, AmountCents: 6999},
		},
	})
	badSplitResp.Body.Close()
	if badSplitResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("split com soma divergente status = %d, esperado 400", badSplitResp.StatusCode)
	}

	splitResp := env.doJSON(t, http.MethodPut, "/api/transactions/"+itoa64(amazonID)+"/splits", map[string]any{
		"splits": []domain.SplitInput{
			{PersonID: selfID, AmountCents: 5000},
			{PersonID: regina.ID, AmountCents: 7000},
		},
	})
	if splitResp.StatusCode != http.StatusOK {
		t.Fatalf("split amazon status = %d, esperado 200", splitResp.StatusCode)
	}
	splitTx := decodeBody[domain.Transaction](t, splitResp)
	if len(splitTx.Splits) != 2 {
		t.Fatalf("len(splits) = %d, esperado 2", len(splitTx.Splits))
	}

	uber1Resp := env.doJSON(t, http.MethodPut, "/api/transactions/"+itoa64(uber1ID)+"/splits", map[string]any{
		"splits": []domain.SplitInput{
			{PersonID: regina.ID, AmountCents: 1990},
		},
	})
	uber1Resp.Body.Close()
	if uber1Resp.StatusCode != http.StatusOK {
		t.Fatalf("split uber1 status = %d, esperado 200", uber1Resp.StatusCode)
	}

	assignRemResp := env.doJSON(t, http.MethodPost, "/api/statements/"+itoa64(stmt.ID)+"/assign-remaining", map[string]any{
		"person_id": selfID,
	})
	if assignRemResp.StatusCode != http.StatusOK {
		t.Fatalf("assign remaining status = %d, esperado 200", assignRemResp.StatusCode)
	}
	assignResult := decodeBody[struct {
		Updated int `json:"updated"`
	}](t, assignRemResp)
	if assignResult.Updated != 1 {
		t.Fatalf("assign remaining updated = %d, esperado 1", assignResult.Updated)
	}

	dashResp := env.doJSON(t, http.MethodGet, "/api/dashboard?year=2025&month=4", nil)
	if dashResp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard status = %d, esperado 200", dashResp.StatusCode)
	}
	dash := decodeBody[domain.DashboardSummary](t, dashResp)
	if dash.TotalCents != 15980 {
		t.Fatalf("dashboard total = %d, esperado 15980", dash.TotalCents)
	}
	if dash.SelfTotalCents != 6990 {
		t.Fatalf("dashboard self_total = %d, esperado 6990", dash.SelfTotalCents)
	}
	if dash.OthersTotalCents != 8990 {
		t.Fatalf("dashboard others_total = %d, esperado 8990", dash.OthersTotalCents)
	}
	if dash.UnassignedCount != 0 {
		t.Fatalf("dashboard unassigned_count = %d, esperado 0", dash.UnassignedCount)
	}

	payResp := env.doJSON(t, http.MethodPost, "/api/payments", map[string]any{
		"person_id":       regina.ID,
		"reference_year":  2025,
		"reference_month": 4,
		"amount_cents":    4000,
		"paid_at":         "2025-04-20",
		"note":            "Pix parcial",
	})
	if payResp.StatusCode != http.StatusCreated {
		t.Fatalf("create payment status = %d, esperado 201", payResp.StatusCode)
	}
	payResp.Body.Close()

	ledgerResp := env.doJSON(t, http.MethodGet, "/api/people/"+itoa64(regina.ID)+"/ledger", nil)
	if ledgerResp.StatusCode != http.StatusOK {
		t.Fatalf("ledger status = %d, esperado 200", ledgerResp.StatusCode)
	}
	ledger := decodeBody[domain.PersonLedger](t, ledgerResp)
	if ledger.TotalSpentCents != 8990 || ledger.TotalPaidCents != 4000 || ledger.BalanceCents != 4990 {
		t.Fatalf("ledger inesperado: %+v", ledger)
	}

	singleBankResp := env.doJSON(t, http.MethodPost, "/api/receipts/preview", map[string]any{
		"person_id":             regina.ID,
		"reference_year":        2025,
		"reference_month":       4,
		"bank_filter":           "nubank",
		"include_prior_balance": true,
	})
	if singleBankResp.StatusCode != http.StatusOK {
		t.Fatalf("receipt preview 1 banco status = %d, esperado 200", singleBankResp.StatusCode)
	}
	singleRec := decodeBody[domain.ReceiptPreview](t, singleBankResp)
	if singleRec.SubtotalCents != 8990 || singleRec.PaidCents != 0 || singleRec.PriorBalanceCents != 0 || singleRec.TotalCents != 8990 {
		t.Fatalf("recibo 1 banco inesperado: %+v", singleRec)
	}
	if strings.Contains(singleRec.HTMLBody, "Pagamentos abatidos") {
		t.Fatalf("recibo de banco unico nao deve listar pagamentos abatidos")
	}

	consResp := env.doJSON(t, http.MethodPost, "/api/receipts/preview", map[string]any{
		"person_id":             regina.ID,
		"reference_year":        2025,
		"reference_month":       4,
		"bank_filter":           "",
		"include_prior_balance": false,
	})
	if consResp.StatusCode != http.StatusOK {
		t.Fatalf("receipt preview consolidado status = %d, esperado 200", consResp.StatusCode)
	}
	consRec := decodeBody[domain.ReceiptPreview](t, consResp)
	if consRec.SubtotalCents != 8990 || consRec.PaidCents != 4000 || consRec.TotalCents != 4990 {
		t.Fatalf("recibo consolidado inesperado: %+v", consRec)
	}
	if !strings.Contains(consRec.HTMLBody, "Pagamentos abatidos") {
		t.Fatalf("recibo consolidado deveria exibir pagamentos abatidos")
	}
	if !strings.Contains(consRec.WhatsAppText, "R$ 49,90") {
		t.Fatalf("texto whatsapp deveria conter saldo final R$ 49,90: %s", consRec.WhatsAppText)
	}

	sendResp := env.doJSON(t, http.MethodPost, "/api/receipts/send", map[string]any{
		"person_id":       regina.ID,
		"reference_year":  2025,
		"reference_month": 4,
		"recipient_email": "regina@example.com",
	})
	if sendResp.StatusCode != http.StatusCreated {
		t.Fatalf("send receipt status = %d, esperado 201", sendResp.StatusCode)
	}
	sentRec := decodeBody[domain.Receipt](t, sendResp)
	if sentRec.DeliveryStatus != "sent" || sentRec.SentAt == nil {
		t.Fatalf("recibo enviado inesperado: %+v", sentRec)
	}

	htmlResp, err := http.Get(env.server.URL + "/api/receipts/" + itoa64(sentRec.ID) + "/html")
	if err != nil {
		t.Fatalf("get receipt html: %v", err)
	}
	htmlBytes, _ := io.ReadAll(htmlResp.Body)
	htmlResp.Body.Close()
	if htmlResp.StatusCode != http.StatusOK {
		t.Fatalf("receipt html status = %d, esperado 200", htmlResp.StatusCode)
	}
	if ct := htmlResp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type inesperado: %q", ct)
	}
	if !strings.Contains(string(htmlBytes), "Regina") {
		t.Fatalf("HTML do recibo nao contem nome da pessoa")
	}

	env.mailer.failErr = errors.New("smtp connection refused")
	failSendResp := env.doJSON(t, http.MethodPost, "/api/receipts/send", map[string]any{
		"person_id":       regina.ID,
		"reference_year":  2025,
		"reference_month": 4,
		"recipient_email": "regina@example.com",
	})
	if failSendResp.StatusCode != http.StatusCreated {
		t.Fatalf("send receipt failed status = %d, esperado 201", failSendResp.StatusCode)
	}
	failedRec := decodeBody[domain.Receipt](t, failSendResp)
	if failedRec.DeliveryStatus != "failed" || failedRec.ErrorMessage == "" {
		t.Fatalf("recibo falho deveria registrar delivery_status=failed e error_message: %+v", failedRec)
	}

	listRecResp := env.doJSON(t, http.MethodGet, "/api/receipts", nil)
	if listRecResp.StatusCode != http.StatusOK {
		t.Fatalf("list receipts status = %d, esperado 200", listRecResp.StatusCode)
	}
	allReceipts := decodeBody[[]domain.Receipt](t, listRecResp)
	if len(allReceipts) != 2 {
		t.Fatalf("len(receipts) = %d, esperado 2", len(allReceipts))
	}
}

func TestImportManualWithoutFileAndDownloadReturns404(t *testing.T) {
	env := newTestEnv(t)

	confirmResp := env.doJSON(t, http.MethodPost, "/api/imports/confirm", map[string]any{
		"bank":            "itau",
		"card_label":      "Manual Black",
		"reference_year":  2025,
		"reference_month": 5,
		"transactions": []map[string]any{
			{
				"purchase_date": "2025-05-03",
				"description":   "Padaria Central",
				"amount_cents":  3450,
			},
		},
	})
	if confirmResp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(confirmResp.Body)
		t.Fatalf("confirm manual status = %d, body = %s", confirmResp.StatusCode, string(raw))
	}
	stmt := decodeBody[domain.Statement](t, confirmResp)
	if stmt.StoredPath != "" {
		t.Fatalf("fatura manual deve ter stored_path vazio, obtido %q", stmt.StoredPath)
	}
	if stmt.FileFormat != "manual" {
		t.Fatalf("fatura manual deve ter file_format=manual, obtido %q", stmt.FileFormat)
	}

	fileResp, err := http.Get(env.server.URL + "/api/statements/" + itoa64(stmt.ID) + "/file")
	if err != nil {
		t.Fatalf("get manual file: %v", err)
	}
	fileResp.Body.Close()
	if fileResp.StatusCode != http.StatusNotFound {
		t.Fatalf("download arquivo fatura manual status = %d, esperado 404", fileResp.StatusCode)
	}
}

func TestAIExtractRequiresConsentAndRejectsScannedPDF(t *testing.T) {
	env := newTestEnv(t)

	blankPDF := []byte("%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Count 1/Kids[3 0 R]>>endobj\n3 0 obj<</Type/Page/MediaBox[0 0 612 792]/Parent 2 0 R>>endobj\nxref\n0 4\n0000000000 65535 f \n0000000009 00000 n \n0000000054 00000 n \n0000000109 00000 n \ntrailer<</Size 4/Root 1 0 R>>\nstartxref\n176\n%%EOF\n")

	prevResp := env.uploadFile(t, "/api/imports/preview", "file", "escaneada.pdf", blankPDF)
	if prevResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(prevResp.Body)
		t.Fatalf("preview blank pdf status = %d, body = %s", prevResp.StatusCode, string(raw))
	}
	preview := decodeBody[domain.PreviewResult](t, prevResp)
	if preview.Status != "needs_fallback" || preview.HasExtractableText {
		t.Fatalf("pdf sem texto deveria retornar needs_fallback e has_extractable_text=false: %+v", preview)
	}

	env.aiMu.Lock()
	callsBefore := env.aiCalls
	env.aiMu.Unlock()
	if callsBefore != 0 {
		t.Fatalf("IA nao pode ser chamada automaticamente no preview")
	}

	aiNoTextResp := env.doJSON(t, http.MethodPost, "/api/imports/preview/"+preview.StagingID+"/ai-extract", nil)
	if aiNoTextResp.StatusCode != http.StatusUnprocessableEntity {
		raw, _ := io.ReadAll(aiNoTextResp.Body)
		t.Fatalf("ai-extract sem texto status = %d, esperado 422, body = %s", aiNoTextResp.StatusCode, string(raw))
	}
	aiNoTextResp.Body.Close()

	rawUnknownCSV := "coluna_x;coluna_y\nCPF 123.456.789-00;Compra Teste R$ 55,00\n"
	textPrevResp := env.uploadFile(t, "/api/imports/preview", "file", "desconhecido.csv", []byte(rawUnknownCSV))
	if textPrevResp.StatusCode != http.StatusOK {
		t.Fatalf("preview csv desconhecido status = %d", textPrevResp.StatusCode)
	}
	textPreview := decodeBody[domain.PreviewResult](t, textPrevResp)
	if textPreview.Status != "needs_fallback" || !textPreview.HasExtractableText {
		t.Fatalf("csv desconhecido deveria ter needs_fallback e has_extractable_text=true: %+v", textPreview)
	}

	aiNoKeyResp := env.doJSON(t, http.MethodPost, "/api/imports/preview/"+textPreview.StagingID+"/ai-extract", nil)
	aiNoKeyResp.Body.Close()
	if aiNoKeyResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("ai-extract sem API key status = %d, esperado 400", aiNoKeyResp.StatusCode)
	}

	putSettingsResp := env.doJSON(t, http.MethodPut, "/api/settings", map[string]any{
		"ai_api_key": "chave-secreta-ia-123",
		"ai_model":   "gemini-2.5-flash",
	})
	putSettingsResp.Body.Close()
	if putSettingsResp.StatusCode != http.StatusOK {
		t.Fatalf("salvar ai_api_key status = %d", putSettingsResp.StatusCode)
	}

	var receivedMaskedText string
	env.aiHandler = func(apiKey, model, maskedText, _ string) (*ai.ExtractResult, error) {
		if apiKey != "chave-secreta-ia-123" || model != "gemini-2.5-flash" {
			t.Errorf("credenciais IA incorretas: key=%q model=%q", apiKey, model)
		}
		receivedMaskedText = maskedText
		return &ai.ExtractResult{
			Bank:           "outro",
			ReferenceYear:  2025,
			ReferenceMonth: 4,
			Transactions: []domain.PreviewTransaction{
				{
					PurchaseDate: "2025-04-11",
					Description:  "COMPRA TESTE 01/03",
					AmountCents:  5500,
				},
			},
		}, nil
	}

	aiOKResp := env.doJSON(t, http.MethodPost, "/api/imports/preview/"+textPreview.StagingID+"/ai-extract", nil)
	if aiOKResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(aiOKResp.Body)
		t.Fatalf("ai-extract valido status = %d, body = %s", aiOKResp.StatusCode, string(raw))
	}
	aiPreview := decodeBody[domain.PreviewResult](t, aiOKResp)
	if aiPreview.Status != "parsed" || aiPreview.ParserUsed != "ai_gemini" || aiPreview.ComputedTotalCents != 5500 {
		t.Fatalf("ai preview inesperado: %+v", aiPreview)
	}
	if strings.Contains(receivedMaskedText, "123.456.789-00") {
		t.Fatalf("CPF nao foi mascarado antes de enviar para IA: %q", receivedMaskedText)
	}
}

func TestSettingsMasksSecretsOnGet(t *testing.T) {
	env := newTestEnv(t)

	putResp := env.doJSON(t, http.MethodPut, "/api/settings", map[string]any{
		"smtp_host":      "smtp.example.com",
		"smtp_port":      587,
		"smtp_user":      "user@example.com",
		"smtp_pass":      "super-secret-smtp-pass",
		"smtp_from_name": "Financeiro",
		"smtp_tls_mode":  "starttls",
		"ai_api_key":     "super-secret-ai-key",
	})
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("put settings status = %d, esperado 200", putResp.StatusCode)
	}
	putResp.Body.Close()

	getResp := env.doJSON(t, http.MethodGet, "/api/settings", nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get settings status = %d, esperado 200", getResp.StatusCode)
	}
	rawBody, err := io.ReadAll(getResp.Body)
	getResp.Body.Close()
	if err != nil {
		t.Fatalf("ler body settings: %v", err)
	}
	bodyStr := string(rawBody)
	if strings.Contains(bodyStr, "super-secret-smtp-pass") || strings.Contains(bodyStr, "super-secret-ai-key") {
		t.Fatalf("GET /api/settings expôs segredos em texto claro: %s", bodyStr)
	}

	var view domain.SettingsView
	if err := json.Unmarshal(rawBody, &view); err != nil {
		t.Fatalf("unmarshal settings view: %v", err)
	}
	if !view.HasSMTPPass || !view.HasAIAPIKey {
		t.Fatalf("flags has_smtp_pass / has_ai_api_key deveriam ser true: %+v", view)
	}

	updateOtherResp := env.doJSON(t, http.MethodPut, "/api/settings", map[string]any{
		"smtp_from_name": "Financeiro Atualizado",
	})
	updatedView := decodeBody[domain.SettingsView](t, updateOtherResp)
	if !updatedView.HasSMTPPass || !updatedView.HasAIAPIKey {
		t.Fatalf("editar outro campo nao deve limpar segredos: %+v", updatedView)
	}

	testSMTPResp := env.doJSON(t, http.MethodPost, "/api/settings/smtp/test", map[string]any{
		"to": "destino@example.com",
	})
	if testSMTPResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(testSMTPResp.Body)
		t.Fatalf("smtp test status = %d, body = %s", testSMTPResp.StatusCode, string(raw))
	}
	testSMTPResp.Body.Close()
	if env.mailer.lastCfg.Password != "super-secret-smtp-pass" {
		t.Fatalf("mailer deveria receber senha salva no banco")
	}

	clearResp := env.doJSON(t, http.MethodPut, "/api/settings", map[string]any{
		"clear_smtp_pass": true,
	})
	clearedView := decodeBody[domain.SettingsView](t, clearResp)
	if clearedView.HasSMTPPass || !clearedView.HasAIAPIKey {
		t.Fatalf("clear_smtp_pass deveria limpar apenas smtp_pass: %+v", clearedView)
	}
}

func TestBackupExportAndRestoreViaHTTP(t *testing.T) {
	env := newTestEnv(t)

	putResp := env.doJSON(t, http.MethodPut, "/api/settings", map[string]any{
		"smtp_pass":  "segredo-local-smtp",
		"ai_api_key": "segredo-local-ia",
	})
	putResp.Body.Close()

	personResp := env.doJSON(t, http.MethodPost, "/api/people", map[string]any{
		"name":  "Carlos",
		"email": "carlos@example.com",
	})
	if personResp.StatusCode != http.StatusCreated {
		t.Fatalf("create carlos status = %d", personResp.StatusCode)
	}
	personResp.Body.Close()

	csvContent := "date,title,amount\n2025-03-05,Mercado Local,85.00\n"
	prevResp := env.uploadFile(t, "/api/imports/preview", "file", "nubank_2025_03.csv", []byte(csvContent))
	prev := decodeBody[domain.PreviewResult](t, prevResp)

	confirmResp := env.doJSON(t, http.MethodPost, "/api/imports/confirm", map[string]any{
		"staging_id":      prev.StagingID,
		"bank":            prev.Bank,
		"reference_year":  2025,
		"reference_month": 3,
		"parser_used":     prev.ParserUsed,
		"transactions": []map[string]any{
			{
				"purchase_date": "2025-03-05",
				"description":   "Mercado Local",
				"amount_cents":  8500,
			},
		},
	})
	stmt := decodeBody[domain.Statement](t, confirmResp)

	exportResp, err := http.Get(env.server.URL + "/api/backup/export?include_secrets=false")
	if err != nil {
		t.Fatalf("export backup: %v", err)
	}
	zipBytes, err := io.ReadAll(exportResp.Body)
	exportResp.Body.Close()
	if err != nil {
		t.Fatalf("ler zip backup: %v", err)
	}
	if exportResp.StatusCode != http.StatusOK {
		t.Fatalf("export backup status = %d, esperado 200", exportResp.StatusCode)
	}
	if ct := exportResp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("Content-Type export = %q, esperado application/zip", ct)
	}
	if cd := exportResp.Header.Get("Content-Disposition"); !strings.Contains(cd, "finance-backup-") || !strings.Contains(cd, ".zip") {
		t.Fatalf("Content-Disposition inesperado: %q", cd)
	}

	tempPersonResp := env.doJSON(t, http.MethodPost, "/api/people", map[string]any{"name": "Temporaria"})
	tempPersonResp.Body.Close()

	delStmtResp := env.doJSON(t, http.MethodDelete, "/api/statements/"+itoa64(stmt.ID), nil)
	delStmtResp.Body.Close()
	if delStmtResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete statement status = %d", delStmtResp.StatusCode)
	}

	restoreResp := env.uploadFile(t, "/api/backup/restore", "file", "backup.zip", zipBytes)
	if restoreResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(restoreResp.Body)
		t.Fatalf("restore backup status = %d, body = %s", restoreResp.StatusCode, string(raw))
	}
	restoreResp.Body.Close()

	peopleResp := env.doJSON(t, http.MethodGet, "/api/people", nil)
	people := decodeBody[[]domain.Person](t, peopleResp)
	hasCarlos := false
	hasTemporaria := false
	for _, p := range people {
		if p.Name == "Carlos" {
			hasCarlos = true
		}
		if p.Name == "Temporaria" {
			hasTemporaria = true
		}
	}
	if !hasCarlos || hasTemporaria {
		t.Fatalf("pessoas apos restore incorretas: hasCarlos=%v hasTemporaria=%v", hasCarlos, hasTemporaria)
	}

	fileResp, err := http.Get(env.server.URL + "/api/statements/" + itoa64(stmt.ID) + "/file")
	if err != nil {
		t.Fatalf("download apos restore: %v", err)
	}
	restoredCSV, _ := io.ReadAll(fileResp.Body)
	fileResp.Body.Close()
	if fileResp.StatusCode != http.StatusOK || string(restoredCSV) != csvContent {
		t.Fatalf("arquivo de fatura nao foi restaurado corretamente: status=%d", fileResp.StatusCode)
	}

	settingsResp := env.doJSON(t, http.MethodGet, "/api/settings", nil)
	settingsView := decodeBody[domain.SettingsView](t, settingsResp)
	if !settingsView.HasSMTPPass || !settingsView.HasAIAPIKey {
		t.Fatalf("segredos locais deveriam ter sido preservados no restore sem segredos: %+v", settingsView)
	}
}

func TestSPAFallbackAndAPINotFound(t *testing.T) {
	env := newTestEnv(t)

	assetResp, err := http.Get(env.server.URL + "/assets/app.js")
	if err != nil {
		t.Fatalf("get asset: %v", err)
	}
	assetBody, _ := io.ReadAll(assetResp.Body)
	assetResp.Body.Close()
	if assetResp.StatusCode != http.StatusOK || !strings.Contains(string(assetBody), "console.log") {
		t.Fatalf("asset inesperado: status=%d body=%s", assetResp.StatusCode, string(assetBody))
	}

	spaResp, err := http.Get(env.server.URL + "/statements/42")
	if err != nil {
		t.Fatalf("get spa route: %v", err)
	}
	spaBody, _ := io.ReadAll(spaResp.Body)
	spaResp.Body.Close()
	if spaResp.StatusCode != http.StatusOK || !strings.Contains(string(spaBody), "SPA") {
		t.Fatalf("fallback SPA falhou: status=%d body=%s", spaResp.StatusCode, string(spaBody))
	}

	notFoundResp, err := http.Get(env.server.URL + "/api/rota-inexistente")
	if err != nil {
		t.Fatalf("get unknown api: %v", err)
	}
	notFoundBody, _ := io.ReadAll(notFoundResp.Body)
	notFoundResp.Body.Close()
	if notFoundResp.StatusCode != http.StatusNotFound || strings.Contains(string(notFoundBody), "SPA") {
		t.Fatalf("rota /api desconhecida deveria retornar 404 JSON: status=%d body=%s", notFoundResp.StatusCode, string(notFoundBody))
	}
}

func itoa64(v int64) string {
	return strings.TrimSpace(jsonNumberString(v))
}

func jsonNumberString(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
