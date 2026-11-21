package domain

import (
	"fmt"
	"strconv"
	"strings"
)

func ParseBRLCents(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("valor monetario vazio")
	}

	negative := false
	if strings.HasPrefix(s, "-") {
		negative = true
		s = strings.TrimSpace(strings.TrimPrefix(s, "-"))
	} else if strings.HasPrefix(s, "+") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "+"))
	}

	s = strings.TrimPrefix(s, "R$")
	s = strings.TrimPrefix(s, "r$")
	s = strings.TrimSpace(s)

	if strings.HasPrefix(s, "-") {
		negative = true
		s = strings.TrimSpace(strings.TrimPrefix(s, "-"))
	} else if strings.HasPrefix(s, "+") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "+"))
	}

	if strings.HasSuffix(s, "-") {
		negative = true
		s = strings.TrimSpace(strings.TrimSuffix(s, "-"))
	}

	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return 0, fmt.Errorf("valor monetario invalido: %q", raw)
	}

	lastComma := strings.LastIndex(s, ",")
	lastDot := strings.LastIndex(s, ".")

	var decimalSep string
	switch {
	case lastComma >= 0 && lastDot >= 0:
		if lastComma > lastDot {
			decimalSep = ","
			s = strings.ReplaceAll(s, ".", "")
		} else {
			decimalSep = "."
			s = strings.ReplaceAll(s, ",", "")
		}
	case lastComma >= 0:
		decimalSep = ","
	case lastDot >= 0:
		afterDot := len(s) - lastDot - 1
		if afterDot == 2 || afterDot == 1 {
			decimalSep = "."
		} else {
			s = strings.ReplaceAll(s, ".", "")
		}
	}

	var intPart, fracPart string
	if decimalSep != "" {
		parts := strings.Split(s, decimalSep)
		if len(parts) != 2 {
			return 0, fmt.Errorf("valor monetario invalido: %q", raw)
		}
		intPart = parts[0]
		fracPart = parts[1]
	} else {
		intPart = s
		fracPart = "00"
	}

	if intPart == "" {
		intPart = "0"
	}
	if len(fracPart) == 0 {
		fracPart = "00"
	} else if len(fracPart) == 1 {
		fracPart += "0"
	} else if len(fracPart) > 2 {
		return 0, fmt.Errorf("valor monetario com mais de 2 casas decimais: %q", raw)
	}

	reais, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil || reais < 0 {
		return 0, fmt.Errorf("valor monetario invalido: %q", raw)
	}
	centavos, err := strconv.ParseInt(fracPart, 10, 64)
	if err != nil || centavos < 0 {
		return 0, fmt.Errorf("valor monetario invalido: %q", raw)
	}

	total := reais*100 + centavos
	if negative {
		total = -total
	}
	return total, nil
}

func FormatBRL(cents int64) string {
	negative := cents < 0
	if negative {
		cents = -cents
	}
	reais := cents / 100
	centavos := cents % 100

	intStr := strconv.FormatInt(reais, 10)
	var b strings.Builder
	if negative {
		b.WriteString("-")
	}
	b.WriteString("R$ ")

	n := len(intStr)
	for i, ch := range intStr {
		if i > 0 && (n-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(ch)
	}
	b.WriteByte(',')
	if centavos < 10 {
		b.WriteByte('0')
	}
	b.WriteString(strconv.FormatInt(centavos, 10))
	return b.String()
}

func NormalizeBank(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, "-", "")
	switch {
	case strings.Contains(s, "nubank") || strings.Contains(s, "nupagamentos"):
		return "nubank"
	case strings.Contains(s, "mercadopago") || strings.Contains(s, "mpago"):
		return "mercadopago"
	case strings.Contains(s, "inter"):
		return "inter"
	case strings.Contains(s, "itau") || strings.Contains(s, "itaú"):
		return "itau"
	case s == "":
		return "outro"
	default:
		return s
	}
}

func BankDisplayName(bank string) string {
	switch NormalizeBank(bank) {
	case "nubank":
		return "Nubank"
	case "mercadopago":
		return "Mercado Pago"
	case "inter":
		return "Inter"
	case "itau":
		return "Itaú"
	case "outro":
		return "Outro"
	default:
		if bank == "" {
			return "Outro"
		}
		return strings.ToUpper(bank[:1]) + bank[1:]
	}
}
