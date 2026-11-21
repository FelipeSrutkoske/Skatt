package parser

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
)

var (
	reInstallmentExplicit = regexp.MustCompile(`(?i)(?:^|[\s\-\(\[]+)(?:PARCELA|PARC\.?)\s*(\d{1,2})\s*(?:/|DE)\s*(\d{1,2})(?:[\)\]]|$|\s)`)
	reInstallmentTrailing = regexp.MustCompile(`(?:^|[\s\-\(\[]+)(\d{1,2})\s*/\s*(\d{1,2})(?:[\)\]]|$)`)
	reInstallmentInline   = regexp.MustCompile(`(?:^|[\s\-\(\[]+)(\d{1,2})\s*/\s*(\d{1,2})\s+`)
	reMultiSpace          = regexp.MustCompile(`\s+`)
	reFilenameYMD         = regexp.MustCompile(`(20\d{2})[-_./](0[1-9]|1[0-2])(?:[-_./]\d{2})?`)
	reFilenameMY          = regexp.MustCompile(`(?:^|[^0-9])(0[1-9]|1[0-2])[-_./](20\d{2})(?:$|[^0-9])`)
	reFilenameMonthName   = regexp.MustCompile(`(?i)(janeiro|fevereiro|marco|abril|maio|junho|julho|agosto|setembro|outubro|novembro|dezembro|jan|fev|mar|abr|mai|jun|jul|ago|set|out|nov|dez)[\s\-_./]*(?:de[\s\-_./]*)?(20\d{2})`)
)

var accentReplacements = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'å': 'a',
	'Á': 'A', 'À': 'A', 'Â': 'A', 'Ã': 'A', 'Ä': 'A', 'Å': 'A',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'É': 'E', 'È': 'E', 'Ê': 'E', 'Ë': 'E',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'Í': 'I', 'Ì': 'I', 'Î': 'I', 'Ï': 'I',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o', 'ø': 'o',
	'Ó': 'O', 'Ò': 'O', 'Ô': 'O', 'Õ': 'O', 'Ö': 'O', 'Ø': 'O',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'Ú': 'U', 'Ù': 'U', 'Û': 'U', 'Ü': 'U',
	'ç': 'c', 'Ç': 'C',
	'ñ': 'n', 'Ñ': 'N',
	'ý': 'y', 'ÿ': 'y', 'Ý': 'Y',
}

func stripAccents(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if rep, ok := accentReplacements[r]; ok {
			b.WriteRune(rep)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func NormalizeMerchantAndInstallment(description string) (string, *int, *int) {
	cleaned := strings.ToUpper(strings.TrimSpace(stripAccents(description)))
	var current *int
	var total *int

	if loc := reInstallmentExplicit.FindStringSubmatchIndex(cleaned); loc != nil {
		cur, err1 := strconv.Atoi(cleaned[loc[2]:loc[3]])
		tot, err2 := strconv.Atoi(cleaned[loc[4]:loc[5]])
		if err1 == nil && err2 == nil && cur >= 1 && cur <= tot && tot >= 1 && tot <= 99 {
			c := cur
			t := tot
			current = &c
			total = &t
			cleaned = cleaned[:loc[0]] + " " + cleaned[loc[1]:]
		}
	}

	if current == nil {
		if loc := reInstallmentTrailing.FindStringSubmatchIndex(cleaned); loc != nil {
			cur, err1 := strconv.Atoi(cleaned[loc[2]:loc[3]])
			tot, err2 := strconv.Atoi(cleaned[loc[4]:loc[5]])
			if err1 == nil && err2 == nil && cur >= 1 && cur <= tot && tot >= 2 && tot <= 99 {
				c := cur
				t := tot
				current = &c
				total = &t
				cleaned = cleaned[:loc[0]] + " " + cleaned[loc[1]:]
			}
		}
	}

	if current == nil {
		if loc := reInstallmentInline.FindStringSubmatchIndex(cleaned); loc != nil {
			cur, err1 := strconv.Atoi(cleaned[loc[2]:loc[3]])
			tot, err2 := strconv.Atoi(cleaned[loc[4]:loc[5]])
			if err1 == nil && err2 == nil && cur >= 1 && cur <= tot && tot >= 2 && tot <= 99 {
				c := cur
				t := tot
				current = &c
				total = &t
				cleaned = cleaned[:loc[0]] + " " + cleaned[loc[1]:]
			}
		}
	}

	var b strings.Builder
	b.Grow(len(cleaned))
	for _, r := range cleaned {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	normalized := strings.TrimSpace(reMultiSpace.ReplaceAllString(b.String(), " "))
	return normalized, current, total
}

func IsPaymentLine(description string) bool {
	s := strings.ToLower(strings.TrimSpace(stripAccents(description)))
	s = reMultiSpace.ReplaceAllString(s, " ")
	if s == "" {
		return false
	}
	if s == "pagamento" || s == "pagto" || s == "pgto" {
		return true
	}

	patterns := []string{
		"pagamento recebido",
		"pagamento de fatura",
		"pagamento da fatura",
		"pagamento fatura",
		"pagamento efetuado",
		"pagamento confirmado",
		"pagamento on line",
		"pagamento online",
		"pagamento via pix",
		"pagamento boleto",
		"pagamento automatico",
		"pagamento cartao",
		"pagamento do cartao",
		"pagamento em ",
		"inclusao de pagamento",
		"credito pagamento",
		"debito automatico fatura",
		"pgto fatura",
		"pagto fatura",
		"pgto cartao",
		"pagto cartao",
		"pgto efetuado",
		"pagto efetuado",
		"pgto recebido",
		"pagto recebido",
		"valor pago",
	}
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func ensureUTF8(data []byte) string {
	data = stripUTF8BOM(data)
	if utf8.Valid(data) {
		return string(data)
	}
	var b strings.Builder
	b.Grow(len(data) * 2)
	for _, ch := range data {
		b.WriteRune(rune(ch))
	}
	return b.String()
}

func stripUTF8BOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

func normalizeMinus(s string) string {
	r := strings.NewReplacer("\u2212", "-", "\u2013", "-", "\u2014", "-")
	return r.Replace(s)
}

func computeTotalCents(txs []domain.PreviewTransaction) int64 {
	var sum int64
	for _, tx := range txs {
		if !tx.IsIgnored {
			sum += tx.AmountCents
		}
	}
	return sum
}

func buildPreviewTransaction(lineIndex int, purchaseDate string, description string, amountCents int64) domain.PreviewTransaction {
	desc := strings.TrimSpace(reMultiSpace.ReplaceAllString(description, " "))
	norm, cur, tot := NormalizeMerchantAndInstallment(desc)
	return domain.PreviewTransaction{
		LineIndex:          lineIndex,
		PurchaseDate:       purchaseDate,
		Description:        desc,
		NormalizedMerchant: norm,
		InstallmentCurrent: cur,
		InstallmentTotal:   tot,
		AmountCents:        amountCents,
		IsIgnored:          IsPaymentLine(desc),
	}
}

func parseMonthPT(tok string) (int, bool) {
	s := strings.ToLower(strings.Trim(strings.TrimSpace(stripAccents(tok)), "."))
	switch s {
	case "jan", "janeiro":
		return 1, true
	case "fev", "fevereiro", "feb":
		return 2, true
	case "mar", "marco":
		return 3, true
	case "abr", "abril", "apr":
		return 4, true
	case "mai", "maio", "may":
		return 5, true
	case "jun", "junho":
		return 6, true
	case "jul", "julho":
		return 7, true
	case "ago", "agosto", "aug":
		return 8, true
	case "set", "setembro", "sep", "sept":
		return 9, true
	case "out", "outubro", "oct":
		return 10, true
	case "nov", "novembro":
		return 11, true
	case "dez", "dezembro", "dec":
		return 12, true
	default:
		return 0, false
	}
}

func inferPeriodFromFilename(filename string) (int, int, bool) {
	base := stripAccents(filepath.Base(filename))
	if m := reFilenameYMD.FindStringSubmatch(base); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		return y, mo, true
	}
	if m := reFilenameMY.FindStringSubmatch(base); m != nil {
		mo, _ := strconv.Atoi(m[1])
		y, _ := strconv.Atoi(m[2])
		return y, mo, true
	}
	if m := reFilenameMonthName.FindStringSubmatch(base); m != nil {
		if mo, ok := parseMonthPT(m[1]); ok {
			y, _ := strconv.Atoi(m[2])
			return y, mo, true
		}
	}
	return 0, 0, false
}

func inferPeriodFromTransactions(txs []domain.PreviewTransaction) (int, int) {
	type ym struct {
		y int
		m int
	}
	counts := make(map[ym]int)
	allCounts := make(map[ym]int)
	for _, tx := range txs {
		t, err := time.Parse("2006-01-02", tx.PurchaseDate)
		if err != nil {
			continue
		}
		key := ym{y: t.Year(), m: int(t.Month())}
		allCounts[key]++
		if !tx.IsIgnored {
			counts[key]++
		}
	}
	target := counts
	if len(target) == 0 {
		target = allCounts
	}
	var best ym
	bestCount := -1
	for k, c := range target {
		if c > bestCount || (c == bestCount && (k.y > best.y || (k.y == best.y && k.m > best.m))) {
			best = k
			bestCount = c
		}
	}
	return best.y, best.m
}

func parseDateFlexible(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	layouts := []string{
		"2006-01-02",
		"02/01/2006",
		"02-01-2006",
		"02/01/06",
		"2006/01/02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02"), true
		}
	}
	return "", false
}

func resolveYearForMonthDay(day int, month int, anchorYear int, anchorMonth int) string {
	y := anchorYear
	if y == 0 {
		y = time.Now().Year()
	}
	if anchorMonth > 0 {
		if month > anchorMonth+1 {
			y--
		} else if anchorMonth == 12 && month == 1 {
			y++
		}
	}
	t := time.Date(y, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if int(t.Month()) != month || t.Day() != day {
		return ""
	}
	return t.Format("2006-01-02")
}
