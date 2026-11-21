package parser

import (
	"encoding/csv"
	"strings"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
)

func ParseItauCSV(data []byte, filename string) (*ParseOutcome, bool) {
	text := ensureUTF8(data)
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
	dateCol, descCol, amountCol := -1, -1, -1
	var declaredTotal *int64

	for i := 0; i < len(records) && i < 15; i++ {
		row := records[i]
		d, de, a := -1, -1, -1
		for idx, cell := range row {
			col := strings.ToLower(strings.TrimSpace(stripAccents(cell)))
			switch {
			case col == "data" || col == "data da compra" || col == "data compra":
				d = idx
			case col == "lancamento" || col == "descricao" || col == "estabelecimento" || col == "historico" || col == "titulo":
				de = idx
			case col == "valor" || col == "valor (r$)" || col == "valor r$" || strings.HasPrefix(col, "valor"):
				a = idx
			}
		}
		if d >= 0 && de >= 0 && a >= 0 {
			headerIdx = i
			dateCol, descCol, amountCol = d, de, a
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
		txs = append(txs, buildPreviewTransaction(len(txs), isoDate, rawDesc, cents))
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
		ParserUsed:         "itau_csv",
		Bank:               "itau",
		ReferenceYear:      year,
		ReferenceMonth:     month,
		DeclaredTotalCents: declaredTotal,
		ComputedTotalCents: computeTotalCents(txs),
		Transactions:       txs,
	}, true
}
