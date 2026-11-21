package parser

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
)

var (
	rePDFHeaderMonthYear = regexp.MustCompile(`(?i)(?:fatura(?:\s+cartao\s+[a-z]+)?(?:\s*[-–:]?\s*|\s+de\s+)|referencia\s*:?\s*)(janeiro|fevereiro|marco|abril|maio|junho|julho|agosto|setembro|outubro|novembro|dezembro|jan|fev|mar|abr|mai|jun|jul|ago|set|out|nov|dez)[\s/\-]+(?:de\s+)?(20\d{2})`)
	rePDFHeaderNumericMY = regexp.MustCompile(`(?i)(?:fatura|referencia|mes\s+de\s+referencia)\s*:?\s*(0[1-9]|1[0-2])/(20\d{2})`)
	rePDFDueDateNumeric  = regexp.MustCompile(`(?i)(?:vencimento|emissao)\s*:?\s*(\d{2})/(\d{2})/(20\d{2})`)
	rePDFDueDateText     = regexp.MustCompile(`(?i)(?:vencimento|emissao)\s*:?\s*(\d{1,2})\s+(?:de\s+)?(jan|fev|mar|abr|mai|jun|jul|ago|set|out|nov|dez|janeiro|fevereiro|marco|abril|maio|junho|julho|agosto|setembro|outubro|novembro|dezembro)\.?\s+(?:de\s+)?(20\d{2})`)
	rePDFDeclaredTotal   = regexp.MustCompile(`(?i)(?:total\s+a\s+pagar|total\s+da\s+fatura|total\s+desta\s+fatura|valor\s+total(?:\s+da\s+fatura)?|o\s+total\s+da\s+sua\s+fatura(?:\s+e)?)\s*:?\s*([-+]?\s*(?:R\$\s*)?[-+]?(?:\d{1,3}(?:\.\d{3})*|\d+),\d{2}-?)`)
	rePDFLineTextDate    = regexp.MustCompile(`(?i)^\s*(\d{1,2})\s+(?:de\s+)?(janeiro|fevereiro|marco|abril|maio|junho|julho|agosto|setembro|outubro|novembro|dezembro|jan|fev|mar|abr|mai|jun|jul|ago|set|out|nov|dez)\.?(?:\s+(?:de\s+)?(20\d{2}))?\s+(.+?)\s+([-+]?\s*(?:R\$\s*)?[-+]?(?:\d{1,3}(?:\.\d{3})*|\d+),\d{2}-?)\s*$`)
	rePDFLineSlashDate   = regexp.MustCompile(`^\s*(\d{2})/(\d{2})(?:/(\d{2,4}))?\s+(.+?)\s+([-+]?\s*(?:R\$\s*)?[-+]?(?:\d{1,3}(?:\.\d{3})*|\d+),\d{2}-?)\s*$`)
	reOrigDatePrefix     = regexp.MustCompile(`(?i)^\d{1,2}\s+(?:de\s+)?\S+\.?(?:\s+(?:de\s+)?20\d{2})?\s+`)
	reCardDotsPrefix     = regexp.MustCompile(`^[•*·●]{2,}\s*\d{4}\s+`)
)

var pdfSummaryNoise = []string{
	"total da fatura",
	"total a pagar",
	"total desta fatura",
	"valor total",
	"pagamento minimo",
	"saldo anterior",
	"fatura anterior",
	"limite total",
	"limite disponivel",
	"total lancamentos",
	"total de compras",
	"total compras",
	"total do cartao",
	"subtotal",
	"resumo da fatura",
}

func ParsePDFText(text string, filename string) (*ParseOutcome, bool) {
	normalizedText := normalizeMinus(text)
	bank := detectPDFBank(normalizedText, filename)
	if bank == "" {
		return nil, false
	}

	explicitYear, explicitMonth, hasExplicit := extractPDFExplicitPeriod(normalizedText)
	anchorYear, anchorMonth := extractPDFAnchorDate(normalizedText)
	if hasExplicit {
		anchorYear = explicitYear
		anchorMonth = explicitMonth
	} else if anchorYear == 0 {
		if fy, fm, ok := inferPeriodFromFilename(filename); ok {
			anchorYear = fy
			anchorMonth = fm
		}
	}

	declaredTotal := extractPDFDeclaredTotal(normalizedText)
	txs := extractPDFTransactions(normalizedText, bank, anchorYear, anchorMonth)
	if len(txs) == 0 {
		return nil, false
	}

	refYear, refMonth := explicitYear, explicitMonth
	if !hasExplicit {
		if fy, fm, ok := inferPeriodFromFilename(filename); ok {
			refYear, refMonth = fy, fm
		} else {
			refYear, refMonth = inferPeriodFromTransactions(txs)
		}
	}
	if refYear == 0 || refMonth == 0 {
		refYear, refMonth = anchorYear, anchorMonth
	}

	return &ParseOutcome{
		Status:             "parsed",
		HasExtractableText: true,
		FileFormat:         "pdf",
		ParserUsed:         bank + "_pdf",
		Bank:               bank,
		ReferenceYear:      refYear,
		ReferenceMonth:     refMonth,
		DeclaredTotalCents: declaredTotal,
		ComputedTotalCents: computeTotalCents(txs),
		Transactions:       txs,
	}, true
}

func detectPDFBank(text string, filename string) string {
	lowerText := strings.ToLower(stripAccents(text))
	switch {
	case strings.Contains(lowerText, "nubank") || strings.Contains(lowerText, "nu pagamentos") || strings.Contains(lowerText, "nu.com.br"):
		return "nubank"
	case strings.Contains(lowerText, "mercado pago") || strings.Contains(lowerText, "mercadopago"):
		return "mercadopago"
	case strings.Contains(lowerText, "banco inter") || strings.Contains(lowerText, "inter s.a") || strings.Contains(lowerText, "cartao inter") || strings.Contains(lowerText, "bancointer"):
		return "inter"
	case strings.Contains(lowerText, "itau") || strings.Contains(lowerText, "itaucard") || strings.Contains(lowerText, "credicard"):
		return "itau"
	}

	lowerFile := strings.ToLower(stripAccents(filename))
	switch {
	case strings.Contains(lowerFile, "nubank"):
		return "nubank"
	case strings.Contains(lowerFile, "mercadopago") || strings.Contains(lowerFile, "mercado_pago") || strings.Contains(lowerFile, "mercado-pago"):
		return "mercadopago"
	case strings.Contains(lowerFile, "inter"):
		return "inter"
	case strings.Contains(lowerFile, "itau"):
		return "itau"
	}
	return ""
}

func extractPDFExplicitPeriod(text string) (int, int, bool) {
	stripped := stripAccents(text)
	if m := rePDFHeaderMonthYear.FindStringSubmatch(stripped); m != nil {
		if mo, ok := parseMonthPT(m[1]); ok {
			y, _ := strconv.Atoi(m[2])
			return y, mo, true
		}
	}
	if m := rePDFHeaderNumericMY.FindStringSubmatch(stripped); m != nil {
		mo, _ := strconv.Atoi(m[1])
		y, _ := strconv.Atoi(m[2])
		return y, mo, true
	}
	return 0, 0, false
}

func extractPDFAnchorDate(text string) (int, int) {
	stripped := stripAccents(text)
	if m := rePDFDueDateNumeric.FindStringSubmatch(stripped); m != nil {
		mo, _ := strconv.Atoi(m[2])
		y, _ := strconv.Atoi(m[3])
		return y, mo
	}
	if m := rePDFDueDateText.FindStringSubmatch(stripped); m != nil {
		if mo, ok := parseMonthPT(m[2]); ok {
			y, _ := strconv.Atoi(m[3])
			return y, mo
		}
	}
	return 0, 0
}

func extractPDFDeclaredTotal(text string) *int64 {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		stripped := stripAccents(line)
		if m := rePDFDeclaredTotal.FindStringSubmatch(stripped); m != nil {
			if c, err := domain.ParseBRLCents(m[1]); err == nil && c >= 0 {
				val := c
				return &val
			}
		}
	}
	return nil
}

func extractPDFTransactions(text string, bank string, anchorYear int, anchorMonth int) []domain.PreviewTransaction {
	lines := strings.Split(text, "\n")
	var txs []domain.PreviewTransaction

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		var isoDate string
		var desc string
		var rawAmt string

		if m := rePDFLineSlashDate.FindStringSubmatch(line); m != nil {
			day, err1 := strconv.Atoi(m[1])
			month, err2 := strconv.Atoi(m[2])
			if err1 != nil || err2 != nil || month < 1 || month > 12 || day < 1 || day > 31 {
				continue
			}
			if m[3] != "" {
				year, err3 := strconv.Atoi(m[3])
				if err3 != nil {
					continue
				}
				if year < 100 {
					year += 2000
				}
				t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
				if int(t.Month()) != month || t.Day() != day {
					continue
				}
				isoDate = t.Format("2006-01-02")
			} else {
				isoDate = resolveYearForMonthDay(day, month, anchorYear, anchorMonth)
			}
			desc = strings.TrimSpace(m[4])
			rawAmt = strings.TrimSpace(m[5])
		} else if m := rePDFLineTextDate.FindStringSubmatch(stripAccents(line)); m != nil {
			day, err1 := strconv.Atoi(m[1])
			month, ok := parseMonthPT(m[2])
			if err1 != nil || !ok || day < 1 || day > 31 {
				continue
			}
			if m[3] != "" {
				year, err3 := strconv.Atoi(m[3])
				if err3 != nil {
					continue
				}
				t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
				if int(t.Month()) != month || t.Day() != day {
					continue
				}
				isoDate = t.Format("2006-01-02")
			} else {
				isoDate = resolveYearForMonthDay(day, month, anchorYear, anchorMonth)
			}
			desc = extractOriginalDesc(line, m[4], m[5])
			rawAmt = strings.TrimSpace(m[5])
		} else {
			continue
		}

		if isoDate == "" || desc == "" {
			continue
		}

		if bank == "nubank" {
			desc = strings.TrimSpace(reCardDotsPrefix.ReplaceAllString(desc, ""))
		}
		if isPDFSummaryNoise(desc) {
			continue
		}

		cents, err := domain.ParseBRLCents(rawAmt)
		if err != nil {
			continue
		}
		txs = append(txs, buildPreviewTransaction(len(txs), isoDate, desc, cents))
	}

	return txs
}

func extractOriginalDesc(origLine string, strippedDesc string, amtToken string) string {
	idx := strings.LastIndex(origLine, amtToken)
	if idx > 0 {
		prefix := strings.TrimSpace(origLine[:idx])
		if loc := reOrigDatePrefix.FindStringIndex(prefix); loc != nil && loc[1] < len(prefix) {
			candidate := strings.TrimSpace(prefix[loc[1]:])
			if candidate != "" {
				return candidate
			}
		}
	}
	return strings.TrimSpace(strippedDesc)
}

func isPDFSummaryNoise(desc string) bool {
	lower := strings.ToLower(strings.TrimSpace(stripAccents(desc)))
	lower = reMultiSpace.ReplaceAllString(lower, " ")
	for _, noise := range pdfSummaryNoise {
		if strings.Contains(lower, noise) {
			return true
		}
	}
	return false
}
