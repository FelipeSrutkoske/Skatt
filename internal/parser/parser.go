package parser

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
)

type ParseOutcome struct {
	Status             string                      `json:"status"`
	HasExtractableText bool                        `json:"has_extractable_text"`
	ExtractedText      string                      `json:"-"`
	TextPreview        string                      `json:"text_preview"`
	FileFormat         string                      `json:"file_format"`
	ParserUsed         string                      `json:"parser_used"`
	Bank               string                      `json:"bank"`
	ReferenceYear      int                         `json:"reference_year"`
	ReferenceMonth     int                         `json:"reference_month"`
	DeclaredTotalCents *int64                      `json:"declared_total_cents"`
	ComputedTotalCents int64                       `json:"computed_total_cents"`
	Transactions       []domain.PreviewTransaction `json:"transactions"`
	Reason             string                      `json:"reason"`
}

func ParseFile(ctx context.Context, filename string, filePath string, password string) (*ParseOutcome, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	format := detectFileFormat(filename, data)

	switch format {
	case "pdf":
		return parsePDFFile(ctx, filename, filePath, password)
	case "ofx":
		return parseOFXFile(filename, data), nil
	case "csv":
		return parseCSVFile(filename, data), nil
	default:
		return buildUnknownFallback(format, data), nil
	}
}

func detectFileFormat(filename string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if bytes.HasPrefix(data, []byte("%PDF-")) || ext == ".pdf" {
		return "pdf"
	}
	sample := strings.ToUpper(string(data[:min(len(data), 4096)]))
	if ext == ".ofx" || ext == ".qfx" || strings.Contains(sample, "<OFX>") || strings.Contains(sample, "<STMTTRN>") {
		return "ofx"
	}
	if ext == ".csv" {
		return "csv"
	}
	if ext != "" {
		return strings.TrimPrefix(ext, ".")
	}
	return "unknown"
}

func parsePDFFile(ctx context.Context, filename string, filePath string, password string) (*ParseOutcome, error) {
	extractedText, err := ExtractPDFText(ctx, filePath, password)
	if err != nil {
		if errors.Is(err, ErrPDFPasswordRequired) {
			return &ParseOutcome{
				Status:     "needs_password",
				FileFormat: "pdf",
				Reason:     "PDF protegido por senha. Informe a senha para desbloquear em memoria.",
			}, nil
		}
		if errors.Is(err, ErrPDFInvalidPassword) {
			return nil, ErrPDFInvalidPassword
		}
		if errors.Is(err, ErrPDFNoText) {
			return &ParseOutcome{
				Status:             "needs_fallback",
				HasExtractableText: false,
				FileFormat:         "pdf",
				Reason:             "PDF sem camada de texto extraivel. Utilize o preenchimento manual.",
			}, nil
		}
		return nil, err
	}

	preview := buildTextPreview(extractedText)
	if out, ok := ParsePDFText(extractedText, filename); ok && len(out.Transactions) > 0 {
		out.Status = "parsed"
		out.HasExtractableText = true
		out.ExtractedText = extractedText
		out.TextPreview = preview
		out.FileFormat = "pdf"
		return out, nil
	}

	return &ParseOutcome{
		Status:             "needs_fallback",
		HasExtractableText: true,
		ExtractedText:      extractedText,
		TextPreview:        preview,
		FileFormat:         "pdf",
		Reason:             "Formato de PDF nao reconhecido pelos parsers locais.",
	}, nil
}

func parseOFXFile(filename string, data []byte) *ParseOutcome {
	text := ensureUTF8(data)
	preview := buildTextPreview(text)
	if out, ok := ParseOFX(data, filename); ok && len(out.Transactions) > 0 {
		out.ExtractedText = text
		out.TextPreview = preview
		return out
	}
	return &ParseOutcome{
		Status:             "needs_fallback",
		HasExtractableText: strings.TrimSpace(text) != "",
		ExtractedText:      text,
		TextPreview:        preview,
		FileFormat:         "ofx",
		Reason:             "Formato OFX nao reconhecido ou sem transacoes validas.",
	}
}

func parseCSVFile(filename string, data []byte) *ParseOutcome {
	text := ensureUTF8(data)
	preview := buildTextPreview(text)

	if out, ok := ParseNubankCSV(data, filename); ok && len(out.Transactions) > 0 {
		out.ExtractedText = text
		out.TextPreview = preview
		return out
	}

	lowerText := strings.ToLower(stripAccents(text))
	lowerFile := strings.ToLower(stripAccents(filename))
	preferItau := (strings.Contains(lowerText, "itau") || strings.Contains(lowerFile, "itau")) &&
		!strings.Contains(lowerText, "inter") && !strings.Contains(lowerFile, "inter")

	if preferItau {
		if out, ok := ParseItauCSV(data, filename); ok && len(out.Transactions) > 0 {
			out.ExtractedText = text
			out.TextPreview = preview
			return out
		}
		if out, ok := ParseInterCSV(data, filename); ok && len(out.Transactions) > 0 {
			out.ExtractedText = text
			out.TextPreview = preview
			return out
		}
	} else {
		if out, ok := ParseInterCSV(data, filename); ok && len(out.Transactions) > 0 {
			out.ExtractedText = text
			out.TextPreview = preview
			return out
		}
		if out, ok := ParseItauCSV(data, filename); ok && len(out.Transactions) > 0 {
			out.ExtractedText = text
			out.TextPreview = preview
			return out
		}
	}

	return &ParseOutcome{
		Status:             "needs_fallback",
		HasExtractableText: strings.TrimSpace(text) != "",
		ExtractedText:      text,
		TextPreview:        preview,
		FileFormat:         "csv",
		Reason:             "Formato CSV nao reconhecido pelos parsers locais.",
	}
}

func buildUnknownFallback(format string, data []byte) *ParseOutcome {
	hasText := false
	var text string
	var preview string
	if len(data) > 0 && bytes.IndexByte(data, 0) < 0 {
		text = ensureUTF8(data)
		if strings.TrimSpace(text) != "" {
			hasText = true
			preview = buildTextPreview(text)
		}
	}
	return &ParseOutcome{
		Status:             "needs_fallback",
		HasExtractableText: hasText,
		ExtractedText:      text,
		TextPreview:        preview,
		FileFormat:         format,
		Reason:             "Formato de arquivo nao suportado pelos parsers locais.",
	}
}

func buildTextPreview(text string) string {
	masked := strings.TrimSpace(MaskPII(text))
	runes := []rune(masked)
	if len(runes) > 500 {
		return string(runes[:500])
	}
	return masked
}
