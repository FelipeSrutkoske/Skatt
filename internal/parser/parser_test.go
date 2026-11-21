package parser

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeMerchantAndInstallment(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantCur *int
		wantTot *int
	}{
		{
			input:   "MAGAZINE LUIZA 02/06",
			want:    "MAGAZINE LUIZA",
			wantCur: intPtr(2),
			wantTot: intPtr(6),
		},
		{
			input:   "Mercado Livre Parcela 3 de 10",
			want:    "MERCADO LIVRE",
			wantCur: intPtr(3),
			wantTot: intPtr(10),
		},
		{
			input:   "LOJAS AMERICANAS PARC 02/06",
			want:    "LOJAS AMERICANAS",
			wantCur: intPtr(2),
			wantTot: intPtr(6),
		},
		{
			input:   "Farmácia São João 2/6",
			want:    "FARMACIA SAO JOAO",
			wantCur: intPtr(2),
			wantTot: intPtr(6),
		},
		{
			input:   "Uber *Trip",
			want:    "UBER TRIP",
			wantCur: nil,
			wantTot: nil,
		},
		{
			input:   "Café   Açaí   Pão",
			want:    "CAFE ACAI PAO",
			wantCur: nil,
			wantTot: nil,
		},
	}

	for _, tc := range tests {
		got, cur, tot := NormalizeMerchantAndInstallment(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeMerchantAndInstallment(%q) merchant = %q, want %q", tc.input, got, tc.want)
		}
		if !sameIntPtr(cur, tc.wantCur) || !sameIntPtr(tot, tc.wantTot) {
			t.Errorf("NormalizeMerchantAndInstallment(%q) installment = (%v, %v), want (%v, %v)", tc.input, cur, tot, tc.wantCur, tc.wantTot)
		}
	}
}

func TestIsPaymentLine(t *testing.T) {
	positives := []string{
		"Pagamento recebido",
		"Pagamento de fatura",
		"PAGAMENTO EFETUADO",
		"Pagamento em 05 ABR",
		"PAGAMENTO DA FATURA",
		"PGTO FATURA",
		"Pagamento on line",
	}
	for _, s := range positives {
		if !IsPaymentLine(s) {
			t.Errorf("IsPaymentLine(%q) = false, want true", s)
		}
	}

	negatives := []string{
		"UBER *TRIP",
		"Estorno Amazon",
		"PAGAMENTO ESTACIONAMENTO SHOPPING",
		"MERCADO LIVRE",
	}
	for _, s := range negatives {
		if IsPaymentLine(s) {
			t.Errorf("IsPaymentLine(%q) = true, want false", s)
		}
	}
}

func TestMaskPII(t *testing.T) {
	raw := strings.Join([]string{
		"Titular: Joao Silva",
		"CPF: 123.456.789-00",
		"CPF 98765432100",
		"Cartao: 4532 1234 5678 9012",
		"Cartao alt: 4532-1234-5678-9012",
		"Cartao junto: 4532123456789012",
		"CEP: 01310-100",
		"Endereco: Av. Paulista, 1000 - Bela Vista",
		"Rua das Flores, 45 - Centro",
		"10/04 UBER *TRIP R$ 120,00",
		"11/04 RESTAURANTE RUA AUGUSTA R$ 85,50",
		"12/04 AMAZON BR R$ 1.234,56",
	}, "\n")

	masked := MaskPII(raw)

	forbidden := []string{
		"123.456.789-00",
		"98765432100",
		"4532 1234 5678 9012",
		"4532-1234-5678-9012",
		"4532123456789012",
		"01310-100",
		"Av. Paulista, 1000",
		"Rua das Flores, 45",
	}
	for _, f := range forbidden {
		if strings.Contains(masked, f) {
			t.Errorf("MaskPII did not mask %q in:\n%s", f, masked)
		}
	}

	requiredTokens := []string{"[CPF]", "[CARTAO]", "[CEP]", "[ENDERECO]"}
	for _, tok := range requiredTokens {
		if !strings.Contains(masked, tok) {
			t.Errorf("MaskPII missing token %q in:\n%s", tok, masked)
		}
	}

	preserved := []string{
		"10/04 UBER *TRIP R$ 120,00",
		"11/04 RESTAURANTE RUA AUGUSTA R$ 85,50",
		"12/04 AMAZON BR R$ 1.234,56",
	}
	for _, p := range preserved {
		if !strings.Contains(masked, p) {
			t.Errorf("MaskPII altered transaction line %q in:\n%s", p, masked)
		}
	}
}

func TestNubankCSVPreservesRepeatedUberAndIgnoresPayment(t *testing.T) {
	csvContent := strings.Join([]string{
		"date,title,amount",
		"2025-04-05,Pagamento recebido,-1500.00",
		"2025-04-10,Uber *Trip,19.90",
		"2025-04-10,Uber *Trip,19.90",
		"2025-04-11,Magazine Luiza - Parcela 2/6,150.00",
		"2025-04-12,Estorno Amazon,-45.00",
	}, "\n")

	tmpFile := filepath.Join(t.TempDir(), "nubank-2025-04.csv")
	if err := os.WriteFile(tmpFile, []byte(csvContent), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	out, err := ParseFile(context.Background(), "nubank-2025-04.csv", tmpFile, "")
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}
	if out.Status != "parsed" {
		t.Fatalf("Status = %q, want parsed", out.Status)
	}
	if out.Bank != "nubank" || out.ParserUsed != "nubank_csv" {
		t.Fatalf("Bank=%q ParserUsed=%q", out.Bank, out.ParserUsed)
	}
	if out.ReferenceYear != 2025 || out.ReferenceMonth != 4 {
		t.Fatalf("Period = %d-%02d, want 2025-04", out.ReferenceYear, out.ReferenceMonth)
	}
	if len(out.Transactions) != 5 {
		t.Fatalf("len(Transactions) = %d, want 5", len(out.Transactions))
	}

	for i, tx := range out.Transactions {
		if tx.LineIndex != i {
			t.Errorf("tx[%d].LineIndex = %d, want %d", i, tx.LineIndex, i)
		}
	}

	if !out.Transactions[0].IsIgnored {
		t.Errorf("payment line should be ignored")
	}
	if out.Transactions[1].AmountCents != 1990 || out.Transactions[2].AmountCents != 1990 {
		t.Errorf("repeated Uber rides not preserved: %d and %d", out.Transactions[1].AmountCents, out.Transactions[2].AmountCents)
	}
	if out.Transactions[1].NormalizedMerchant != "UBER TRIP" || out.Transactions[2].NormalizedMerchant != "UBER TRIP" {
		t.Errorf("Uber normalized merchants = %q, %q", out.Transactions[1].NormalizedMerchant, out.Transactions[2].NormalizedMerchant)
	}
	if !sameIntPtr(out.Transactions[3].InstallmentCurrent, intPtr(2)) || !sameIntPtr(out.Transactions[3].InstallmentTotal, intPtr(6)) {
		t.Errorf("Magazine Luiza installment = (%v, %v), want (2, 6)", out.Transactions[3].InstallmentCurrent, out.Transactions[3].InstallmentTotal)
	}
	if out.Transactions[4].AmountCents != -4500 || out.Transactions[4].IsIgnored {
		t.Errorf("Estorno Amazon = amount %d ignored %v, want -4500 false", out.Transactions[4].AmountCents, out.Transactions[4].IsIgnored)
	}

	wantTotal := int64(1990 + 1990 + 15000 - 4500)
	if out.ComputedTotalCents != wantTotal {
		t.Errorf("ComputedTotalCents = %d, want %d", out.ComputedTotalCents, wantTotal)
	}
}

func TestInterCSVItauCSVAndOFX(t *testing.T) {
	t.Run("InterCSV_Semicolon", func(t *testing.T) {
		content := strings.Join([]string{
			"Data;Lançamento;Categoria;Tipo;Valor",
			"10/04/2025;SUPERMERCADO BH;Mercado;Compra à vista;R$ 1.234,56",
			"12/04/2025;DROGARIA ARAUJO PARC 02/06;Saúde;Compra parcelada;R$ 50,00",
			"15/04/2025;PAGAMENTO DE FATURA;Pagamentos;Pagamento;-R$ 1.000,00",
		}, "\n")
		p := filepath.Join(t.TempDir(), "fatura_inter_2025_04.csv")
		if err := os.WriteFile(p, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		out, err := ParseFile(context.Background(), "fatura_inter_2025_04.csv", p, "")
		if err != nil {
			t.Fatalf("ParseFile: %v", err)
		}
		if out.Status != "parsed" || out.Bank != "inter" || out.ParserUsed != "inter_csv" {
			t.Fatalf("got status=%q bank=%q parser=%q", out.Status, out.Bank, out.ParserUsed)
		}
		if len(out.Transactions) != 3 {
			t.Fatalf("len=%d, want 3", len(out.Transactions))
		}
		if out.Transactions[0].AmountCents != 123456 || out.Transactions[0].PurchaseDate != "2025-04-10" {
			t.Errorf("tx0 = %+v", out.Transactions[0])
		}
		if !sameIntPtr(out.Transactions[1].InstallmentCurrent, intPtr(2)) || !sameIntPtr(out.Transactions[1].InstallmentTotal, intPtr(6)) {
			t.Errorf("tx1 installment = (%v,%v)", out.Transactions[1].InstallmentCurrent, out.Transactions[1].InstallmentTotal)
		}
		if !out.Transactions[2].IsIgnored {
			t.Errorf("tx2 payment should be ignored")
		}
		if out.ComputedTotalCents != 128456 {
			t.Errorf("ComputedTotalCents = %d, want 128456", out.ComputedTotalCents)
		}
	})

	t.Run("InterCSV_Comma", func(t *testing.T) {
		content := strings.Join([]string{
			"Data,Lançamento,Categoria,Tipo,Valor",
			"10/04/2025,PADARIA CENTRAL,Alimentacao,Compra à vista,\"R$ 42,50\"",
		}, "\n")
		p := filepath.Join(t.TempDir(), "inter_abril.csv")
		if err := os.WriteFile(p, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		out, err := ParseFile(context.Background(), "inter_abril.csv", p, "")
		if err != nil {
			t.Fatalf("ParseFile: %v", err)
		}
		if out.Status != "parsed" || out.Bank != "inter" || out.ComputedTotalCents != 4250 {
			t.Fatalf("got %+v", out)
		}
	})

	t.Run("ItauCSV", func(t *testing.T) {
		content := strings.Join([]string{
			"data,lançamento,valor",
			"08/04/2025,CARREFOUR EXPRESS,\"189,90\"",
			"09/04/2025,MAGAZINE LUIZA 03/06,\"120,00\"",
			"10/04/2025,ESTORNO CARREFOUR,\"-19,90\"",
			"05/04/2025,PAGAMENTO EFETUADO,\"-950,00\"",
		}, "\n")
		p := filepath.Join(t.TempDir(), "fatura_itau_2025_04.csv")
		if err := os.WriteFile(p, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		out, err := ParseFile(context.Background(), "fatura_itau_2025_04.csv", p, "")
		if err != nil {
			t.Fatalf("ParseFile: %v", err)
		}
		if out.Status != "parsed" || out.Bank != "itau" || out.ParserUsed != "itau_csv" {
			t.Fatalf("got status=%q bank=%q parser=%q", out.Status, out.Bank, out.ParserUsed)
		}
		if len(out.Transactions) != 4 {
			t.Fatalf("len=%d, want 4", len(out.Transactions))
		}
		if out.Transactions[2].AmountCents != -1990 || out.Transactions[2].IsIgnored {
			t.Errorf("tx2 = %+v", out.Transactions[2])
		}
		if !out.Transactions[3].IsIgnored {
			t.Errorf("tx3 payment should be ignored")
		}
		if out.ComputedTotalCents != 29000 {
			t.Errorf("ComputedTotalCents = %d, want 29000", out.ComputedTotalCents)
		}
	})

	t.Run("OFX", func(t *testing.T) {
		content := `OFXHEADER:100
DATA:OFXSGML
<OFX>
<SIGNONMSGSRSV1>
<SONRS>
<FI>
<ORG>NU PAGAMENTOS S.A.
</FI>
</SONRS>
</SIGNONMSGSRSV1>
<CREDITCARDMSGSRSV1>
<CCSTMTTRNRS>
<CCSTMTRS>
<BANKTRANLIST>
<STMTTRN>
<TRNTYPE>DEBIT
<DTPOSTED>20250410120000[-3:BRT]
<TRNAMT>-19.90
<MEMO>UBER *TRIP
</STMTTRN>
<STMTTRN>
<TRNTYPE>DEBIT
<DTPOSTED>20250410120500[-3:BRT]
<TRNAMT>-19.90
<MEMO>UBER *TRIP
</STMTTRN>
<STMTTRN>
<TRNTYPE>DEBIT
<DTPOSTED>20250412100000[-3:BRT]
<TRNAMT>-200.00
<MEMO>AMAZON BR 02/04
</STMTTRN>
<STMTTRN>
<TRNTYPE>CREDIT
<DTPOSTED>20250415100000[-3:BRT]
<TRNAMT>50.00
<MEMO>ESTORNO AMAZON
</STMTTRN>
<STMTTRN>
<TRNTYPE>CREDIT
<DTPOSTED>20250405100000[-3:BRT]
<TRNAMT>1200.00
<MEMO>Pagamento recebido
</STMTTRN>
</BANKTRANLIST>
</CCSTMTRS>
</CCSTMTTRNRS>
</CREDITCARDMSGSRSV1>
</OFX>`
		p := filepath.Join(t.TempDir(), "fatura.ofx")
		if err := os.WriteFile(p, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		out, err := ParseFile(context.Background(), "fatura.ofx", p, "")
		if err != nil {
			t.Fatalf("ParseFile: %v", err)
		}
		if out.Status != "parsed" || out.Bank != "nubank" || out.ParserUsed != "ofx_generic" {
			t.Fatalf("got status=%q bank=%q parser=%q", out.Status, out.Bank, out.ParserUsed)
		}
		if len(out.Transactions) != 5 {
			t.Fatalf("len=%d, want 5", len(out.Transactions))
		}
		if out.Transactions[0].AmountCents != 1990 || out.Transactions[1].AmountCents != 1990 {
			t.Errorf("repeated Uber OFX not preserved: %d, %d", out.Transactions[0].AmountCents, out.Transactions[1].AmountCents)
		}
		if out.Transactions[3].AmountCents != -5000 || out.Transactions[3].IsIgnored {
			t.Errorf("Estorno OFX = %+v", out.Transactions[3])
		}
		if !out.Transactions[4].IsIgnored {
			t.Errorf("Pagamento recebido OFX should be ignored")
		}
		wantTotal := int64(1990 + 1990 + 20000 - 5000)
		if out.ComputedTotalCents != wantTotal {
			t.Errorf("ComputedTotalCents = %d, want %d", out.ComputedTotalCents, wantTotal)
		}
	})
}

func TestPDFBankParsers(t *testing.T) {
	t.Run("NubankPDF", func(t *testing.T) {
		text := strings.Join([]string{
			"Nubank",
			"FATURA DE ABRIL 2025",
			"Vencimento: 10 MAI 2025",
			"Total a pagar                                       R$ 244,80",
			"TRANSAÇÕES DE 01 ABR A 30 ABR",
			"05 ABR    Pagamento em 05 ABR                       -R$ 1.200,00",
			"10 ABR    •••• 1234  Uber *Trip                        R$ 19,90",
			"10 ABR    •••• 1234  Uber *Trip                        R$ 19,90",
			"11 ABR    •••• 1234  Magazine Luiza - Parcela 2/6     R$ 250,00",
			"15 ABR    Estorno Amazon                              -R$ 45,00",
			"Total da fatura                                     R$ 244,80",
		}, "\n")

		out, ok := ParsePDFText(text, "fatura_nubank.pdf")
		if !ok {
			t.Fatalf("ParsePDFText failed for Nubank")
		}
		if out.Bank != "nubank" || out.ParserUsed != "nubank_pdf" {
			t.Fatalf("Bank=%q ParserUsed=%q", out.Bank, out.ParserUsed)
		}
		if out.ReferenceYear != 2025 || out.ReferenceMonth != 4 {
			t.Fatalf("Period=%d-%02d, want 2025-04", out.ReferenceYear, out.ReferenceMonth)
		}
		if out.DeclaredTotalCents == nil || *out.DeclaredTotalCents != 24480 {
			t.Fatalf("DeclaredTotalCents = %v, want 24480", out.DeclaredTotalCents)
		}
		if len(out.Transactions) != 5 {
			t.Fatalf("len=%d, want 5", len(out.Transactions))
		}
		if out.ComputedTotalCents != 24480 {
			t.Errorf("ComputedTotalCents = %d, want 24480", out.ComputedTotalCents)
		}
		if !out.Transactions[0].IsIgnored {
			t.Errorf("tx0 payment should be ignored")
		}
		if !sameIntPtr(out.Transactions[3].InstallmentCurrent, intPtr(2)) || !sameIntPtr(out.Transactions[3].InstallmentTotal, intPtr(6)) {
			t.Errorf("tx3 installment = (%v,%v)", out.Transactions[3].InstallmentCurrent, out.Transactions[3].InstallmentTotal)
		}
	})

	t.Run("MercadoPagoPDF", func(t *testing.T) {
		text := strings.Join([]string{
			"Mercado Pago",
			"Cartão de Crédito - Fatura de Abril 2025",
			"Total da fatura: R$ 151,90",
			"Data         Descrição                             Valor",
			"10/04/2025   Mercado Livre Parcela 3 de 10         R$ 89,90",
			"14/04/2025   IFOOD *RESTAURANTE                    R$ 62,00",
			"02/04/2025   Pagamento recebido                   -R$ 500,00",
		}, "\n")

		out, ok := ParsePDFText(text, "mp_2025_04.pdf")
		if !ok {
			t.Fatalf("ParsePDFText failed for Mercado Pago")
		}
		if out.Bank != "mercadopago" || out.ParserUsed != "mercadopago_pdf" {
			t.Fatalf("Bank=%q ParserUsed=%q", out.Bank, out.ParserUsed)
		}
		if out.DeclaredTotalCents == nil || *out.DeclaredTotalCents != 15190 {
			t.Fatalf("DeclaredTotalCents=%v, want 15190", out.DeclaredTotalCents)
		}
		if len(out.Transactions) != 3 || out.ComputedTotalCents != 15190 {
			t.Fatalf("txs=%d total=%d", len(out.Transactions), out.ComputedTotalCents)
		}
		if !sameIntPtr(out.Transactions[0].InstallmentCurrent, intPtr(3)) || !sameIntPtr(out.Transactions[0].InstallmentTotal, intPtr(10)) {
			t.Errorf("tx0 installment = (%v,%v)", out.Transactions[0].InstallmentCurrent, out.Transactions[0].InstallmentTotal)
		}
	})

	t.Run("InterPDF", func(t *testing.T) {
		text := strings.Join([]string{
			"Banco Inter S.A.",
			"Fatura Cartão Inter - Abril/2025",
			"Vencimento: 10/05/2025",
			"Total da fatura                                    R$ 370,80",
			"10 de abr. 2025   SUPERMERCADO BH                  R$ 245,80",
			"12/04/2025        DROGARIA ARAUJO PARC 02/06       R$ 150,00",
			"14/04/2025        ESTORNO DROGARIA                 -R$ 25,00",
			"03/04/2025        PAGAMENTO DE FATURA            -R$ 900,00",
		}, "\n")

		out, ok := ParsePDFText(text, "inter_fatura.pdf")
		if !ok {
			t.Fatalf("ParsePDFText failed for Inter")
		}
		if out.Bank != "inter" || out.ParserUsed != "inter_pdf" {
			t.Fatalf("Bank=%q ParserUsed=%q", out.Bank, out.ParserUsed)
		}
		if out.ReferenceYear != 2025 || out.ReferenceMonth != 4 {
			t.Fatalf("Period=%d-%02d", out.ReferenceYear, out.ReferenceMonth)
		}
		if out.DeclaredTotalCents == nil || *out.DeclaredTotalCents != 37080 {
			t.Fatalf("DeclaredTotalCents=%v", out.DeclaredTotalCents)
		}
		if len(out.Transactions) != 4 || out.ComputedTotalCents != 37080 {
			t.Fatalf("txs=%d total=%d", len(out.Transactions), out.ComputedTotalCents)
		}
		if out.Transactions[0].PurchaseDate != "2025-04-10" {
			t.Errorf("tx0 date = %q, want 2025-04-10", out.Transactions[0].PurchaseDate)
		}
	})

	t.Run("ItauPDF", func(t *testing.T) {
		text := strings.Join([]string{
			"Itaú Unibanco - Itaucard",
			"Vencimento: 10/05/2025",
			"Total desta fatura                                 194,40",
			"Lançamentos: compras e saques",
			"08/04   MAGAZINE LUIZA 02/06                       150,00",
			"11/04   CARREFOUR EXPRESS                           89,40",
			"12/04   ESTORNO CARREFOUR                           45,00-",
			"04/04   PAGAMENTO EFETUADO                       1.100,00-",
			"Total lançamentos atuais                           194,40",
		}, "\n")

		out, ok := ParsePDFText(text, "itaucard.pdf")
		if !ok {
			t.Fatalf("ParsePDFText failed for Itau")
		}
		if out.Bank != "itau" || out.ParserUsed != "itau_pdf" {
			t.Fatalf("Bank=%q ParserUsed=%q", out.Bank, out.ParserUsed)
		}
		if out.ReferenceYear != 2025 || out.ReferenceMonth != 4 {
			t.Fatalf("Period=%d-%02d, want 2025-04", out.ReferenceYear, out.ReferenceMonth)
		}
		if out.DeclaredTotalCents == nil || *out.DeclaredTotalCents != 19440 {
			t.Fatalf("DeclaredTotalCents=%v, want 19440", out.DeclaredTotalCents)
		}
		if len(out.Transactions) != 4 || out.ComputedTotalCents != 19440 {
			t.Fatalf("txs=%d total=%d", len(out.Transactions), out.ComputedTotalCents)
		}
		if out.Transactions[2].AmountCents != -4500 {
			t.Errorf("tx2 trailing minus = %d, want -4500", out.Transactions[2].AmountCents)
		}
		if !out.Transactions[3].IsIgnored {
			t.Errorf("tx3 payment should be ignored")
		}
	})
}

func TestPDFPasswordAndEmptyTextDetection(t *testing.T) {
	origRunner := pdfToTextRunner
	t.Cleanup(func() {
		pdfToTextRunner = origRunner
	})

	dummyPDF := filepath.Join(t.TempDir(), "fatura.pdf")
	if err := os.WriteFile(dummyPDF, []byte("%PDF-1.4\n"), 0600); err != nil {
		t.Fatal(err)
	}

	t.Run("PasswordRequired", func(t *testing.T) {
		pdfToTextRunner = func(ctx context.Context, pdfPath string, password string) (string, string, error) {
			return "", "Command Line Error: Incorrect password\n", errors.New("exit status 1")
		}
		_, err := ExtractPDFText(context.Background(), dummyPDF, "")
		if !errors.Is(err, ErrPDFPasswordRequired) {
			t.Fatalf("err = %v, want ErrPDFPasswordRequired", err)
		}
		out, err := ParseFile(context.Background(), "fatura.pdf", dummyPDF, "")
		if err != nil {
			t.Fatalf("ParseFile err = %v", err)
		}
		if out.Status != "needs_password" {
			t.Fatalf("Status = %q, want needs_password", out.Status)
		}
	})

	t.Run("InvalidPasswordNeverLeaksSecret", func(t *testing.T) {
		secretPass := "Sup3rS3cretPDFPass!"
		pdfToTextRunner = func(ctx context.Context, pdfPath string, password string) (string, string, error) {
			return "", "Command Line Error: Incorrect password\n", errors.New("exit status 1")
		}
		_, err := ExtractPDFText(context.Background(), dummyPDF, secretPass)
		if !errors.Is(err, ErrPDFInvalidPassword) {
			t.Fatalf("err = %v, want ErrPDFInvalidPassword", err)
		}
		if strings.Contains(err.Error(), secretPass) {
			t.Fatalf("error message leaked password: %v", err)
		}
		_, err = ParseFile(context.Background(), "fatura.pdf", dummyPDF, secretPass)
		if !errors.Is(err, ErrPDFInvalidPassword) {
			t.Fatalf("ParseFile err = %v, want ErrPDFInvalidPassword", err)
		}
		if strings.Contains(err.Error(), secretPass) {
			t.Fatalf("ParseFile error message leaked password: %v", err)
		}
	})

	t.Run("ScannedEmptyText", func(t *testing.T) {
		pdfToTextRunner = func(ctx context.Context, pdfPath string, password string) (string, string, error) {
			return "   \f\n\t  ", "", nil
		}
		_, err := ExtractPDFText(context.Background(), dummyPDF, "")
		if !errors.Is(err, ErrPDFNoText) {
			t.Fatalf("err = %v, want ErrPDFNoText", err)
		}
		out, err := ParseFile(context.Background(), "fatura.pdf", dummyPDF, "")
		if err != nil {
			t.Fatalf("ParseFile err = %v", err)
		}
		if out.Status != "needs_fallback" || out.HasExtractableText {
			t.Fatalf("got status=%q hasText=%v, want needs_fallback false", out.Status, out.HasExtractableText)
		}
	})

	t.Run("UnlockedWithValidPassword", func(t *testing.T) {
		pdfToTextRunner = func(ctx context.Context, pdfPath string, password string) (string, string, error) {
			if password != "123456" {
				return "", "Command Line Error: Incorrect password\n", errors.New("exit status 1")
			}
			return "Nubank\nFATURA DE ABRIL 2025\n10 ABR   Uber *Trip   R$ 19,90\n", "", nil
		}
		out, err := ParseFile(context.Background(), "fatura.pdf", dummyPDF, "123456")
		if err != nil {
			t.Fatalf("ParseFile err = %v", err)
		}
		if out.Status != "parsed" || len(out.Transactions) != 1 || out.ComputedTotalCents != 1990 {
			t.Fatalf("got %+v", out)
		}
	})
}

func TestUnknownFormatReturnsNeedsFallbackWithoutExternalCalls(t *testing.T) {
	t.Run("UnknownCSV", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "random.csv")
		if err := os.WriteFile(p, []byte("foo,bar,baz\n1,2,3\n"), 0600); err != nil {
			t.Fatal(err)
		}
		out, err := ParseFile(context.Background(), "random.csv", p, "")
		if err != nil {
			t.Fatalf("ParseFile err = %v", err)
		}
		if out.Status != "needs_fallback" || !out.HasExtractableText {
			t.Fatalf("got status=%q hasText=%v", out.Status, out.HasExtractableText)
		}
	})

	t.Run("UnknownPDFBank", func(t *testing.T) {
		origRunner := pdfToTextRunner
		t.Cleanup(func() {
			pdfToTextRunner = origRunner
		})
		pdfToTextRunner = func(ctx context.Context, pdfPath string, password string) (string, string, error) {
			return "BANCO DESCONHECIDO XYZ\nRelatorio qualquer sem padrao\n", "", nil
		}
		p := filepath.Join(t.TempDir(), "outro.pdf")
		if err := os.WriteFile(p, []byte("%PDF-1.4\n"), 0600); err != nil {
			t.Fatal(err)
		}
		out, err := ParseFile(context.Background(), "outro.pdf", p, "")
		if err != nil {
			t.Fatalf("ParseFile err = %v", err)
		}
		if out.Status != "needs_fallback" || !out.HasExtractableText {
			t.Fatalf("got status=%q hasText=%v", out.Status, out.HasExtractableText)
		}
	})
}

func TestStagingManager(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), "staging")
	uploadsDir := filepath.Join(t.TempDir(), "uploads")
	mgr := NewStagingManager(baseDir)

	payload := []byte("date,title,amount\n2025-04-10,Uber *Trip,19.90\n")
	sum := sha256.Sum256(payload)
	wantHash := hex.EncodeToString(sum[:])

	meta, err := mgr.Save("../../etc/passwd/../fatura abril!.csv", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if meta.ID == "" {
		t.Fatalf("empty staging ID")
	}
	if meta.FileSHA256 != wantHash {
		t.Fatalf("FileSHA256 = %q, want %q", meta.FileSHA256, wantHash)
	}
	if strings.Contains(meta.SafeFilename, "..") || strings.ContainsAny(meta.SafeFilename, `/\`) {
		t.Fatalf("unsafe filename: %q", meta.SafeFilename)
	}
	if !strings.HasPrefix(meta.FilePath, baseDir) {
		t.Fatalf("FilePath %q escaped baseDir %q", meta.FilePath, baseDir)
	}

	loaded, err := mgr.LoadMeta(meta.ID)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if loaded.FileSHA256 != wantHash {
		t.Fatalf("loaded hash mismatch")
	}

	if _, err := mgr.LoadMeta("../escape"); err == nil {
		t.Fatalf("LoadMeta allowed traversal id")
	}

	if err := mgr.SaveExtractedText(meta.ID, "texto extraido"); err != nil {
		t.Fatalf("SaveExtractedText: %v", err)
	}
	gotText, err := mgr.ReadExtractedText(meta.ID)
	if err != nil || gotText != "texto extraido" {
		t.Fatalf("ReadExtractedText = %q, %v", gotText, err)
	}

	relPath, err := mgr.PromoteToUploads(meta.ID, uploadsDir, 2025, 4, 42)
	if err != nil {
		t.Fatalf("PromoteToUploads: %v", err)
	}
	if !strings.HasPrefix(relPath, "2025/04/42_") {
		t.Fatalf("relPath = %q, want prefix 2025/04/42_", relPath)
	}
	promotedFull := filepath.Join(uploadsDir, filepath.FromSlash(relPath))
	gotBytes, err := os.ReadFile(promotedFull)
	if err != nil || !bytes.Equal(gotBytes, payload) {
		t.Fatalf("promoted file mismatch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(baseDir, meta.ID)); !os.IsNotExist(err) {
		t.Fatalf("staging directory should be removed after promote")
	}

	metaOld, err := mgr.Save("old.csv", strings.NewReader("a,b,c\n"))
	if err != nil {
		t.Fatal(err)
	}
	oldDir := filepath.Join(baseDir, metaOld.ID)
	oldMetaPath := filepath.Join(oldDir, "meta.json")
	oldTime := time.Now().UTC().Add(-48 * time.Hour)
	oldJSON := strings.Replace(
		mustReadString(t, oldMetaPath),
		metaOld.CreatedAt,
		oldTime.Format(time.RFC3339),
		1,
	)
	if err := os.WriteFile(oldMetaPath, []byte(oldJSON), 0600); err != nil {
		t.Fatal(err)
	}

	metaFresh, err := mgr.Save("fresh.csv", strings.NewReader("x,y,z\n"))
	if err != nil {
		t.Fatal(err)
	}

	if err := mgr.CleanupExpired(24 * time.Hour); err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("expired staging dir was not cleaned up")
	}
	if _, err := os.Stat(filepath.Join(baseDir, metaFresh.ID)); err != nil {
		t.Fatalf("fresh staging dir was removed unexpectedly: %v", err)
	}

	if err := mgr.Remove(metaFresh.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(baseDir, metaFresh.ID)); !os.IsNotExist(err) {
		t.Fatalf("Remove did not delete staging dir")
	}
}

func intPtr(v int) *int {
	return &v
}

func sameIntPtr(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func mustReadString(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
