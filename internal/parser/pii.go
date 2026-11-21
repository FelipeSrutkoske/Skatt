package parser

import (
	"regexp"
	"strings"
)

var (
	reFormattedCPF = regexp.MustCompile(`\b\d{3}\.\d{3}\.\d{3}-\d{2}\b`)
	reLabeledCPF   = regexp.MustCompile(`(?i)(\bCPF(?:\s*/\s*CNPJ)?\s*[:\-]?\s*)\d{11}\b`)
	reCardGrouped  = regexp.MustCompile(`\b(?:\d{4}[-\s]\d{4}[-\s]\d{4}[-\s]\d{1,7}|\d{4}[-\s]\d{6}[-\s]\d{5})\b`)
	reCardPlain    = regexp.MustCompile(`\b\d{13,19}\b`)
	reCEPFormatted = regexp.MustCompile(`\b\d{5}-\d{3}\b`)
	reCEPLabeled   = regexp.MustCompile(`(?i)(\bCEP\s*[:\-]?\s*)\d{8}\b`)
	reTxDatePrefix = regexp.MustCompile(`^\s*(?:\d{2}/\d{2}(?:/\d{2,4})?|\d{4}-\d{2}-\d{2}|\d{2}\s+[A-Za-z]{3})\b`)
	reBRLAmount    = regexp.MustCompile(`(?i)R\$\s*-?\d`)
)

var addressPrefixes = []string{
	"ENDERECO:",
	"ENDERECO ",
	"LOGRADOURO:",
	"LOGRADOURO ",
	"RUA ",
	"AV. ",
	"AV ",
	"AVENIDA ",
	"ALAMEDA ",
	"TRAVESSA ",
	"RODOVIA ",
	"ESTRADA ",
	"PRACA ",
	"BAIRRO:",
	"COMPLEMENTO:",
}

func MaskPII(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if isExplicitAddressLine(line) {
			lines[i] = "[ENDERECO]"
			continue
		}
		masked := reFormattedCPF.ReplaceAllString(line, "[CPF]")
		masked = reLabeledCPF.ReplaceAllString(masked, "${1}[CPF]")
		masked = reCardGrouped.ReplaceAllStringFunc(masked, maskCardCandidate)
		masked = reCardPlain.ReplaceAllString(masked, "[CARTAO]")
		masked = reCEPFormatted.ReplaceAllString(masked, "[CEP]")
		masked = reCEPLabeled.ReplaceAllString(masked, "${1}[CEP]")
		lines[i] = masked
	}
	return strings.Join(lines, "\n")
}

func maskCardCandidate(match string) string {
	digits := 0
	for _, r := range match {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	if digits >= 13 && digits <= 19 {
		return "[CARTAO]"
	}
	return match
}

func isExplicitAddressLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	if reTxDatePrefix.MatchString(trimmed) || reBRLAmount.MatchString(trimmed) {
		return false
	}
	upper := strings.ToUpper(stripAccents(trimmed))
	for _, prefix := range addressPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}
