package parser

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
)

var (
	reOFXStmtTrn = regexp.MustCompile(`(?is)<STMTTRN>(.*?)</STMTTRN>`)
	reOFXTag     = regexp.MustCompile(`(?i)<([A-Z0-9_.]+)>([^<\r\n]*)`)
)

type rawOFXTx struct {
	trnType     string
	isoDate     string
	description string
	rawCents    int64
	isPayment   bool
}

func ParseOFX(data []byte, filename string) (*ParseOutcome, bool) {
	text := ensureUTF8(data)
	upper := strings.ToUpper(text)
	if !strings.Contains(upper, "<STMTTRN>") {
		return nil, false
	}

	blocks := reOFXStmtTrn.FindAllStringSubmatch(text, -1)
	if len(blocks) == 0 {
		return nil, false
	}

	org := extractOFXField(text, "ORG")
	bank := detectOFXBank(org, filename)

	var rawTxs []rawOFXTx
	for _, b := range blocks {
		body := b[1]
		trnType := strings.ToUpper(strings.TrimSpace(extractOFXField(body, "TRNTYPE")))
		dtPosted := strings.TrimSpace(extractOFXField(body, "DTPOSTED"))
		trnAmt := strings.TrimSpace(normalizeMinus(extractOFXField(body, "TRNAMT")))
		memo := strings.TrimSpace(extractOFXField(body, "MEMO"))
		name := strings.TrimSpace(extractOFXField(body, "NAME"))

		desc := memo
		if desc == "" {
			desc = name
		}
		if desc == "" {
			continue
		}

		isoDate, ok := parseOFXDate(dtPosted)
		if !ok {
			continue
		}
		cents, err := domain.ParseBRLCents(trnAmt)
		if err != nil {
			continue
		}

		rawTxs = append(rawTxs, rawOFXTx{
			trnType:     trnType,
			isoDate:     isoDate,
			description: desc,
			rawCents:    cents,
			isPayment:   IsPaymentLine(desc),
		})
	}

	if len(rawTxs) == 0 {
		return nil, false
	}

	invertSign := shouldInvertOFXSigns(rawTxs)
	txs := make([]domain.PreviewTransaction, 0, len(rawTxs))
	for i, rt := range rawTxs {
		amt := rt.rawCents
		if invertSign {
			amt = -amt
		}
		txs = append(txs, buildPreviewTransaction(i, rt.isoDate, rt.description, amt))
	}

	year, month, ok := inferPeriodFromFilename(filename)
	if !ok {
		year, month = inferPeriodFromTransactions(txs)
	}
	if year == 0 || month == 0 {
		if dtEnd, ok := parseOFXDate(extractOFXField(text, "DTEND")); ok {
			if t, err := time.Parse("2006-01-02", dtEnd); err == nil {
				year = t.Year()
				month = int(t.Month())
			}
		}
	}

	return &ParseOutcome{
		Status:             "parsed",
		HasExtractableText: true,
		FileFormat:         "ofx",
		ParserUsed:         "ofx_generic",
		Bank:               bank,
		ReferenceYear:      year,
		ReferenceMonth:     month,
		ComputedTotalCents: computeTotalCents(txs),
		Transactions:       txs,
	}, true
}

func extractOFXField(block string, tag string) string {
	want := strings.ToUpper(tag)
	matches := reOFXTag.FindAllStringSubmatch(block, -1)
	for _, m := range matches {
		if strings.ToUpper(m[1]) == want {
			val := strings.TrimSpace(m[2])
			if val != "" {
				return val
			}
		}
	}
	return ""
}

func parseOFXDate(raw string) (string, bool) {
	digits := make([]byte, 0, 8)
	for i := 0; i < len(raw) && len(digits) < 8; i++ {
		if raw[i] >= '0' && raw[i] <= '9' {
			digits = append(digits, raw[i])
		} else {
			break
		}
	}
	if len(digits) != 8 {
		return "", false
	}
	y, err1 := strconv.Atoi(string(digits[0:4]))
	m, err2 := strconv.Atoi(string(digits[4:6]))
	d, err3 := strconv.Atoi(string(digits[6:8]))
	if err1 != nil || err2 != nil || err3 != nil {
		return "", false
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	if t.Year() != y || int(t.Month()) != m || t.Day() != d {
		return "", false
	}
	return t.Format("2006-01-02"), true
}

func shouldInvertOFXSigns(rawTxs []rawOFXTx) bool {
	negNonPay := 0
	posNonPay := 0
	negDebit := 0
	posDebit := 0
	for _, rt := range rawTxs {
		if rt.isPayment {
			continue
		}
		if rt.trnType == "DEBIT" {
			if rt.rawCents < 0 {
				negDebit++
			} else if rt.rawCents > 0 {
				posDebit++
			}
		}
		if rt.rawCents < 0 {
			negNonPay++
		} else if rt.rawCents > 0 {
			posNonPay++
		}
	}
	if negDebit > posDebit {
		return true
	}
	if posDebit > negDebit {
		return false
	}
	return negNonPay > posNonPay
}

func detectOFXBank(org string, filename string) string {
	for _, candidate := range []string{org, filename} {
		s := strings.ToLower(stripAccents(candidate))
		switch {
		case strings.Contains(s, "nubank") || strings.Contains(s, "nu pagamentos"):
			return "nubank"
		case strings.Contains(s, "mercadopago") || strings.Contains(s, "mercado pago"):
			return "mercadopago"
		case strings.Contains(s, "inter"):
			return "inter"
		case strings.Contains(s, "itau"):
			return "itau"
		}
	}
	if strings.TrimSpace(org) != "" {
		return domain.NormalizeBank(org)
	}
	return "outro"
}
