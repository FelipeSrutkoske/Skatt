package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
)

const (
	defaultGeminiModel   = "gemini-2.5-flash"
	defaultGeminiBaseURL = "https://generativelanguage.googleapis.com"
	defaultHTTPTimeout   = 45 * time.Second
	maxResponseBytes     = 4 << 20
	maxErrorBodyBytes    = 2048
)

type ExtractResult struct {
	Bank               string
	ReferenceYear      int
	ReferenceMonth     int
	DeclaredTotalCents *int64
	Transactions       []domain.PreviewTransaction
}

type Extractor interface {
	ExtractFromText(ctx context.Context, maskedText string, filename string) (*ExtractResult, error)
}

type GeminiClient struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

func NewGeminiClient(apiKey, model, baseURL string) *GeminiClient {
	m := strings.TrimSpace(model)
	if m == "" {
		m = defaultGeminiModel
	}
	b := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if b == "" {
		b = defaultGeminiBaseURL
	}
	return &GeminiClient{
		apiKey:  strings.TrimSpace(apiKey),
		model:   m,
		baseURL: b,
		httpClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
	}
}

type geminiRequest struct {
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
	Contents          []geminiContent `json:"contents"`
	GenerationConfig  geminiGenConfig `json:"generationConfig"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenConfig struct {
	ResponseMimeType string  `json:"responseMimeType"`
	Temperature      float64 `json:"temperature"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

type rawExtractPayload struct {
	Bank               string                  `json:"bank"`
	ReferenceYear      int                     `json:"reference_year"`
	ReferenceMonth     int                     `json:"reference_month"`
	DeclaredTotalCents *flexibleCents          `json:"declared_total_cents"`
	Transactions       []rawExtractTransaction `json:"transactions"`
}

type rawExtractTransaction struct {
	PurchaseDate       string        `json:"purchase_date"`
	Description        string        `json:"description"`
	AmountCents        flexibleCents `json:"amount_cents"`
	IsIgnored          bool          `json:"is_ignored"`
	InstallmentCurrent *int          `json:"installment_current,omitempty"`
	InstallmentTotal   *int          `json:"installment_total,omitempty"`
}

type flexibleCents struct {
	value int64
	valid bool
}

func (f *flexibleCents) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		f.value = 0
		f.valid = false
		return nil
	}
	if strings.HasPrefix(trimmed, "\"") {
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return err
		}
		str = strings.TrimSpace(str)
		if str == "" {
			f.value = 0
			f.valid = false
			return nil
		}
		if v, err := strconv.ParseInt(str, 10, 64); err == nil {
			f.value = v
			f.valid = true
			return nil
		}
		v, err := domain.ParseBRLCents(str)
		if err != nil {
			return fmt.Errorf("valor monetario invalido: %q", str)
		}
		f.value = v
		f.valid = true
		return nil
	}
	if v, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		f.value = v
		f.valid = true
		return nil
	}
	if dot := strings.IndexByte(trimmed, '.'); dot >= 0 {
		intPart := trimmed[:dot]
		fracPart := trimmed[dot+1:]
		allZero := fracPart != ""
		for _, ch := range fracPart {
			if ch != '0' {
				allZero = false
				break
			}
		}
		if allZero {
			v, err := strconv.ParseInt(intPart, 10, 64)
			if err == nil {
				f.value = v
				f.valid = true
				return nil
			}
		}
	}
	return fmt.Errorf("valor em centavos invalido: %s", trimmed)
}

func (c *GeminiClient) ExtractFromText(ctx context.Context, maskedText string, filename string) (*ExtractResult, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("chave da API Gemini nao configurada")
	}
	sanitizedText := MaskPII(strings.TrimSpace(maskedText))
	if sanitizedText == "" {
		return nil, fmt.Errorf("texto extraido vazio para analise por IA")
	}

	prompt := buildExtractionPrompt(sanitizedText, strings.TrimSpace(filename))
	payload := geminiRequest{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: extractionSystemInstruction}},
		},
		Contents: []geminiContent{
			{Parts: []geminiPart{{Text: prompt}}},
		},
		GenerationConfig: geminiGenConfig{
			ResponseMimeType: "application/json",
			Temperature:      0,
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("falha ao serializar requisicao Gemini: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent", c.baseURL, url.PathEscape(c.model))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("falha ao criar requisicao Gemini: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("falha na comunicacao com a API Gemini: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errSnippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil, fmt.Errorf("API Gemini retornou status %d: %s", resp.StatusCode, strings.TrimSpace(string(errSnippet)))
	}

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("falha ao ler resposta da API Gemini: %w", err)
	}

	var apiResp geminiResponse
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("resposta invalida da API Gemini: %w", err)
	}
	if apiResp.Error != nil {
		return nil, fmt.Errorf("erro da API Gemini (%d): %s", apiResp.Error.Code, apiResp.Error.Message)
	}
	if len(apiResp.Candidates) == 0 || len(apiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("API Gemini nao retornou conteudo estruturado")
	}

	var combined strings.Builder
	for _, part := range apiResp.Candidates[0].Content.Parts {
		if part.Text != "" {
			combined.WriteString(part.Text)
		}
	}

	return parseExtractPayload(combined.String())
}

const extractionSystemInstruction = `Voce e um extrator rigoroso de faturas de cartao de credito brasileiras.
Retorne EXCLUSIVAMENTE um objeto JSON valido com o esquema:
{
  "bank": "nubank" | "mercadopago" | "inter" | "itau" | "outro",
  "reference_year": 2025,
  "reference_month": 4,
  "declared_total_cents": 123456,
  "transactions": [
    {
      "purchase_date": "2025-04-10",
      "description": "UBER *TRIP",
      "amount_cents": 1990,
      "is_ignored": false
    }
  ]
}
Regras obrigatorias:
- Todos os valores monetarios devem ser inteiros em centavos (ex: R$ 19,90 -> 1990, estorno -R$ 45,00 -> -4500).
- Preserve TODAS as linhas de compra na ordem original, mesmo quando houver compras identicas no mesmo dia e valor.
- Marque pagamentos da propria fatura (ex: "Pagamento recebido", "Pagamento efetuado") com "is_ignored": true.
- Datas em formato ISO YYYY-MM-DD.
- Se o total declarado da fatura nao estiver presente, use null em "declared_total_cents".`

func buildExtractionPrompt(sanitizedText, filename string) string {
	var b strings.Builder
	b.WriteString("Extraia os metadados e todas as transacoes da fatura abaixo.\n")
	if filename != "" {
		b.WriteString("Nome do arquivo: ")
		b.WriteString(filename)
		b.WriteString("\n")
	}
	b.WriteString("\nTexto mascarado da fatura:\n")
	b.WriteString(sanitizedText)
	return b.String()
}

func parseExtractPayload(rawText string) (*ExtractResult, error) {
	cleaned := stripJSONCodeFence(rawText)
	if cleaned == "" {
		return nil, fmt.Errorf("conteudo JSON vazio retornado pela API Gemini")
	}

	var payload rawExtractPayload
	if err := json.Unmarshal([]byte(cleaned), &payload); err != nil {
		return nil, fmt.Errorf("falha ao interpretar JSON retornado pela API Gemini: %w", err)
	}

	refYear := payload.ReferenceYear
	if refYear < 1900 || refYear > 2200 {
		refYear = 0
	}
	refMonth := payload.ReferenceMonth
	if refMonth < 1 || refMonth > 12 {
		refMonth = 0
	}

	var declaredTotal *int64
	if payload.DeclaredTotalCents != nil && payload.DeclaredTotalCents.valid {
		v := payload.DeclaredTotalCents.value
		declaredTotal = &v
	}

	txs := make([]domain.PreviewTransaction, 0, len(payload.Transactions))
	lineIndex := 0
	for _, item := range payload.Transactions {
		desc := strings.TrimSpace(item.Description)
		if desc == "" && !item.AmountCents.valid {
			continue
		}
		if !item.AmountCents.valid {
			return nil, fmt.Errorf("transacao sem valor valido: %q", desc)
		}

		normalized, instCur, instTotal := normalizeMerchantAndInstallment(desc)
		if item.InstallmentCurrent != nil && item.InstallmentTotal != nil && *item.InstallmentCurrent > 0 && *item.InstallmentTotal >= *item.InstallmentCurrent {
			cVal := *item.InstallmentCurrent
			tVal := *item.InstallmentTotal
			instCur = &cVal
			instTotal = &tVal
		}

		txs = append(txs, domain.PreviewTransaction{
			LineIndex:          lineIndex,
			PurchaseDate:       normalizeDate(item.PurchaseDate),
			Description:        desc,
			NormalizedMerchant: normalized,
			InstallmentCurrent: instCur,
			InstallmentTotal:   instTotal,
			AmountCents:        item.AmountCents.value,
			IsIgnored:          item.IsIgnored,
			Splits:             []domain.SplitInput{},
		})
		lineIndex++
	}

	return &ExtractResult{
		Bank:               domain.NormalizeBank(payload.Bank),
		ReferenceYear:      refYear,
		ReferenceMonth:     refMonth,
		DeclaredTotalCents: declaredTotal,
		Transactions:       txs,
	}, nil
}

func stripJSONCodeFence(raw string) string {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		if idx := strings.IndexByte(s, '\n'); idx >= 0 {
			s = s[idx+1:]
		}
		s = strings.TrimSpace(s)
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	return s
}

var (
	cpfFormattedRe   = regexp.MustCompile(`\b\d{3}\.\d{3}\.\d{3}-\d{2}\b`)
	cpfLabeledRawRe  = regexp.MustCompile(`(?i)(\bcpf\s*[:\-]?\s*)\d{11}\b`)
	cardNumberRe     = regexp.MustCompile(`\b(?:\d{4}[\s-]\d{4}[\s-]\d{4}[\s-]\d{1,7}|\d{4}[\s-]\d{6}[\s-]\d{5}|\d{13,19})\b`)
	cepRe            = regexp.MustCompile(`\b\d{5}-\d{3}\b`)
	labeledAddressRe = regexp.MustCompile(`(?im)^[ \t]*(?:endere[cç]o|logradouro)\s*[:\-].*$`)
	streetAddressRe  = regexp.MustCompile(`(?im)^[ \t]*(?:rua|av\.|avenida|alameda|travessa|estrada|rodovia)\s+.*(?:\[CEP\]|,?\s*(?:n[°º.]?\s*)?\d{1,5}\b).*$`)
	brDateRe         = regexp.MustCompile(`^(\d{2})/(\d{2})/(\d{4})$`)
	isoDateRe        = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	installmentRe    = regexp.MustCompile(`(?i)\b(?:parc(?:ela)?\.?\s*)?(\d{1,2})\s*(?:/|de)\s*(\d{1,2})\b`)
	multiSpaceRe     = regexp.MustCompile(`\s+`)
)

func MaskPII(text string) string {
	masked := cpfFormattedRe.ReplaceAllString(text, "[CPF]")
	masked = cpfLabeledRawRe.ReplaceAllString(masked, "${1}[CPF]")
	masked = cardNumberRe.ReplaceAllString(masked, "[CARTAO]")
	masked = cepRe.ReplaceAllString(masked, "[CEP]")
	masked = labeledAddressRe.ReplaceAllString(masked, "[ENDERECO]")
	masked = streetAddressRe.ReplaceAllString(masked, "[ENDERECO]")
	return masked
}

func normalizeDate(raw string) string {
	s := strings.TrimSpace(raw)
	if isoDateRe.MatchString(s) {
		return s
	}
	if m := brDateRe.FindStringSubmatch(s); len(m) == 4 {
		return fmt.Sprintf("%s-%s-%s", m[3], m[2], m[1])
	}
	return s
}

func normalizeMerchantAndInstallment(description string) (string, *int, *int) {
	cleaned := strings.TrimSpace(description)
	var instCur, instTotal *int

	matches := installmentRe.FindAllStringSubmatchIndex(cleaned, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		loc := matches[i]
		cVal, err1 := strconv.Atoi(cleaned[loc[2]:loc[3]])
		tVal, err2 := strconv.Atoi(cleaned[loc[4]:loc[5]])
		if err1 == nil && err2 == nil && cVal >= 1 && tVal >= 2 && cVal <= tVal && tVal <= 99 {
			instCur = &cVal
			instTotal = &tVal
			cleaned = strings.TrimSpace(cleaned[:loc[0]] + " " + cleaned[loc[1]:])
			break
		}
	}

	var b strings.Builder
	for _, r := range strings.ToUpper(removeAccents(cleaned)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	normalized := strings.TrimSpace(multiSpaceRe.ReplaceAllString(b.String(), " "))
	return normalized, instCur, instTotal
}

func removeAccents(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case 'á', 'à', 'â', 'ã', 'ä', 'Á', 'À', 'Â', 'Ã', 'Ä':
			b.WriteByte('A')
		case 'é', 'è', 'ê', 'ë', 'É', 'È', 'Ê', 'Ë':
			b.WriteByte('E')
		case 'í', 'ì', 'î', 'ï', 'Í', 'Ì', 'Î', 'Ï':
			b.WriteByte('I')
		case 'ó', 'ò', 'ô', 'õ', 'ö', 'Ó', 'Ò', 'Ô', 'Õ', 'Ö':
			b.WriteByte('O')
		case 'ú', 'ù', 'û', 'ü', 'Ú', 'Ù', 'Û', 'Ü':
			b.WriteByte('U')
		case 'ç', 'Ç':
			b.WriteByte('C')
		case 'ñ', 'Ñ':
			b.WriteByte('N')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
