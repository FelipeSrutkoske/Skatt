package parser

import (
	"encoding/csv"
	"strings"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
)

func ParseNubankCSV(data []byte, filename string) (*ParseOutcome, bool) {
	text := ensureUTF8(data)
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	records, err := r.ReadAll()
	if err != nil || len(records) < 2 {
		return nil, false
	}

	headerIdx := -1
	dateCol, titleCol, amountCol := -1, -1, -1
	for i := 0; i < len(records) && i < 5; i++ {
		row := records[i]
		d, ti, a := -1, -1, -1
		for idx, cell := range row {
			col := strings.ToLower(strings.TrimSpace(stripAccents(cell)))
			switch col {
			case "date":
				d = idx
			case "title":
				ti = idx
			case "amount":
				a = idx
			}
		}
		if d >= 0 && ti >= 0 && a >= 0 {
			headerIdx = i
			dateCol, titleCol, amountCol = d, ti, a
			break
		}
	}
	if headerIdx < 0 {
		return nil, false
	}

	var txs []domain.PreviewTransaction
	for i := headerIdx + 1; i < len(records); i++ {
		row := records[i]
		maxCol := max(dateCol, max(titleCol, amountCol))
		if len(row) <= maxCol {
			continue
		}
		rawDate := strings.TrimSpace(row[dateCol])
		rawTitle := strings.TrimSpace(row[titleCol])
		rawAmount := strings.TrimSpace(normalizeMinus(row[amountCol]))
		if rawDate == "" && rawTitle == "" && rawAmount == "" {
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
		txs = append(txs, buildPreviewTransaction(len(txs), isoDate, rawTitle, cents))
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
		ParserUsed:         "nubank_csv",
		Bank:               "nubank",
		ReferenceYear:      year,
		ReferenceMonth:     month,
		ComputedTotalCents: computeTotalCents(txs),
		Transactions:       txs,
	}, true
}
