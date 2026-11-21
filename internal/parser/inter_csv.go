package parser

import (
	"encoding/csv"
	"strings"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
)

func ParseInterCSV(data []byte, filename string) (*ParseOutcome, bool) {
	text := ensureUTF8(data)
	lowerText := strings.ToLower(stripAccents(text))
	lowerFile := strings.ToLower(stripAccents(filename))

	delim := detectCSVDelimiter(text)
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = delim
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	records, err := r.ReadAll()
	if err != nil || len(records) < 2 {
		return nil, false
	}

	headerIdx := -1
	dateCol, descCol, amountCol, typeCol := -1, -1, -1, -1
	hasInterCols := false
	var declaredTotal *int64

	for i := 0; i < len(records) && i < 15; i++ {
		row := records[i]
		d, de, a, ty := -1, -1, -1, -1
		interSignal := false
		for idx, cell := range row {
			col := strings.ToLower(strings.TrimSpace(stripAccents(cell)))
			switch {
			case col == "data" || col == "data da compra" || col == "data compra":
				d = idx
			case col == "lancamento" || col == "descricao" || col == "estabelecimento" || col == "historico":
				de = idx
			case col == "valor" || col == "valor (r$)" || col == "valor r$" || strings.HasPrefix(col, "valor"):
				a = idx
			case col == "categoria":
				interSignal = true
			case col == "tipo":
				ty = idx
				interSignal = true
			}
		}
		if d >= 0 && de >= 0 && a >= 0 {
			headerIdx = i
			dateCol, descCol, amountCol, typeCol = d, de, a, ty
			hasInterCols = interSignal
			break
		}
		if total, ok := extractCSVMetadataTotal(row); ok {
			t := total
			declaredTotal = &t
		}
	}

	if headerIdx < 0 {
		return nil, false
	}

	mentionsInter := strings.Contains(lowerText, "inter") || strings.Contains(lowerFile, "inter")
	mentionsItau := strings.Contains(lowerText, "itau") || strings.Contains(lowerFile, "itau")
	hasRS := strings.Contains(text, "R$")

	if !hasInterCols && !mentionsInter {
		if mentionsItau || delim != ';' || !hasRS {
			return nil, false
		}
	}

	var txs []domain.PreviewTransaction
	for i := headerIdx + 1; i < len(records); i++ {
		row := records[i]
		maxCol := max(dateCol, max(descCol, amountCol))
		if len(row) <= maxCol {
			continue
		}
		rawDate := strings.TrimSpace(row[dateCol])
		rawDesc := strings.TrimSpace(row[descCol])
		rawAmount := strings.TrimSpace(normalizeMinus(row[amountCol]))
		if rawDate == "" && rawDesc == "" {
			continue
		}
		isoDate, ok := parseDateFlexible(rawDate)
		if !ok {
			continue
		}
		cents, err := domain.ParseBRLCents(rawAmount)
		if err != nil {
			continue
		}
		desc := rawDesc
		if typeCol >= 0 && typeCol < len(row) {
			rawType := strings.TrimSpace(row[typeCol])
			if rawType != "" {
				_, cur, _ := NormalizeMerchantAndInstallment(desc)
				if cur == nil {
					_, tCur, _ := NormalizeMerchantAndInstallment(rawType)
					if tCur != nil {
						desc = desc + " " + rawType
					}
				}
			}
		}
		txs = append(txs, buildPreviewTransaction(len(txs), isoDate, desc, cents))
	}

	if len(txs) == 0 {
		return nil, false
	}

	year, month, ok := inferPeriodFromFilename(filename)
	if !ok {
		year, month = inferPeriodFromTransactions(txs)
	}

	return &ParseOutcome{
		Status:             "parsed",
		HasExtractableText: true,
		FileFormat:         "csv",
		ParserUsed:         "inter_csv",
		Bank:               "inter",
		ReferenceYear:      year,
		ReferenceMonth:     month,
		DeclaredTotalCents: declaredTotal,
		ComputedTotalCents: computeTotalCents(txs),
		Transactions:       txs,
	}, true
}

func detectCSVDelimiter(text string) rune {
	lines := strings.Split(text, "\n")
	semiCount := 0
	commaCount := 0
	checked := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		semiCount += strings.Count(trimmed, ";")
		commaCount += strings.Count(trimmed, ",")
		checked++
		if checked >= 10 {
			break
		}
	}
	if semiCount >= commaCount && semiCount > 0 {
		return ';'
	}
	return ','
}

func extractCSVMetadataTotal(row []string) (int64, bool) {
	if len(row) < 2 {
		return 0, false
	}
	label := strings.ToLower(strings.TrimSpace(stripAccents(row[0])))
	if strings.Contains(label, "total") && !strings.Contains(label, "limite") {
		for _, cell := range row[1:] {
			if c, err := domain.ParseBRLCents(normalizeMinus(cell)); err == nil {
				return c, true
			}
		}
	}
	return 0, false
}
