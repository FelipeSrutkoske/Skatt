package receipt

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
)

var monthNamesPT = [...]string{
	"",
	"Janeiro",
	"Fevereiro",
	"Março",
	"Abril",
	"Maio",
	"Junho",
	"Julho",
	"Agosto",
	"Setembro",
	"Outubro",
	"Novembro",
	"Dezembro",
}

func monthNamePT(month int) string {
	if month >= 1 && month <= 12 {
		return monthNamesPT[month]
	}
	return fmt.Sprintf("%02d", month)
}

func formatPeriodPT(year, month int) string {
	return fmt.Sprintf("%s/%04d", monthNamePT(month), year)
}

func formatDateBR(iso string) string {
	s := strings.TrimSpace(iso)
	if len(s) == 10 && s[4] == '-' && s[7] == '-' {
		return s[8:10] + "/" + s[5:7] + "/" + s[0:4]
	}
	return s
}

func formatInstallment(current, total *int) string {
	if current == nil || total == nil || *current <= 0 || *total <= 0 {
		return ""
	}
	return fmt.Sprintf("%02d/%02d", *current, *total)
}

func sanitizeSingleLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func BuildSubject(data domain.ReceiptPreview) string {
	period := formatPeriodPT(data.ReferenceYear, data.ReferenceMonth)
	person := sanitizeSingleLine(data.PersonName)
	bankFilter := strings.TrimSpace(data.BankFilter)

	var parts []string
	if bankFilter != "" {
		parts = append(parts, "Resumo de fatura", domain.BankDisplayName(bankFilter), period)
	} else {
		parts = append(parts, "Resumo de faturas", period)
	}
	if person != "" {
		parts = append(parts, person)
	}
	return strings.Join(parts, " - ")
}

type htmlReceiptItem struct {
	Date        string
	BankDisplay string
	Description string
	Installment string
	SplitNote   string
	AmountBRL   string
	IsNegative  bool
}

type htmlReceiptView struct {
	Subject          string
	PersonName       string
	PeriodLabel      string
	ScopeLabel       string
	SingleBank       bool
	ShowBankColumn   bool
	Items            []htmlReceiptItem
	SubtotalBRL      string
	PaidBRL          string
	ShowPriorBalance bool
	PriorBalanceBRL  string
	TotalBRL         string
}

var receiptHTMLTemplate = template.Must(template.New("receipt").Parse(`<!DOCTYPE html>
<html lang="pt-BR">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.Subject}}</title>
<style>
body {
  margin: 0;
  padding: 24px 16px;
  background-color: #f8fafc;
  color: #0f172a;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
  font-size: 14px;
  line-height: 1.5;
}
.tabular-nums {
  font-variant-numeric: tabular-nums;
}
@media print {
  body {
    background-color: #ffffff !important;
    padding: 0 !important;
  }
  .receipt-sheet {
    border: none !important;
    max-width: none !important;
    padding: 0 !important;
  }
  tr {
    page-break-inside: avoid;
  }
}
</style>
</head>
<body style="margin:0;padding:24px 16px;background-color:#f8fafc;color:#0f172a;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:14px;line-height:1.5;">
<div class="receipt-sheet" style="max-width:720px;margin:0 auto;background-color:#ffffff;border:1px solid #e2e8f0;padding:28px 32px;">
  <div style="border-bottom:1px solid #cbd5e1;padding-bottom:16px;margin-bottom:20px;">
    <h1 style="margin:0 0 12px 0;font-size:18px;font-weight:600;color:#0f172a;letter-spacing:-0.01em;">Resumo de despesas</h1>
    <table role="presentation" cellpadding="0" cellspacing="0" style="width:100%;border-collapse:collapse;font-size:13px;color:#334155;">
      <tr>
        <td style="padding:2px 0;width:110px;color:#64748b;">Pessoa</td>
        <td style="padding:2px 0;font-weight:600;color:#0f172a;">{{.PersonName}}</td>
      </tr>
      <tr>
        <td style="padding:2px 0;color:#64748b;">Período</td>
        <td style="padding:2px 0;color:#0f172a;">{{.PeriodLabel}}</td>
      </tr>
      <tr>
        <td style="padding:2px 0;color:#64748b;">Escopo</td>
        <td style="padding:2px 0;color:#0f172a;">{{.ScopeLabel}}</td>
      </tr>
    </table>
  </div>

  <table cellpadding="0" cellspacing="0" style="width:100%;border-collapse:collapse;font-size:13px;">
    <thead>
      <tr style="background-color:#f8fafc;border-top:1px solid #e2e8f0;border-bottom:1px solid #cbd5e1;">
        <th align="left" style="padding:8px 10px;font-weight:600;color:#475569;text-align:left;white-space:nowrap;">Data</th>
        {{if .ShowBankColumn}}<th align="left" style="padding:8px 10px;font-weight:600;color:#475569;text-align:left;white-space:nowrap;">Banco</th>{{end}}
        <th align="left" style="padding:8px 10px;font-weight:600;color:#475569;text-align:left;">Descrição</th>
        <th align="right" style="padding:8px 10px;font-weight:600;color:#475569;text-align:right;white-space:nowrap;">Valor</th>
      </tr>
    </thead>
    <tbody>
      {{if .Items}}
        {{range .Items}}
        <tr style="border-bottom:1px solid #e2e8f0;">
          <td class="tabular-nums" style="padding:8px 10px;color:#475569;white-space:nowrap;vertical-align:top;font-variant-numeric:tabular-nums;">{{.Date}}</td>
          {{if $.ShowBankColumn}}<td style="padding:8px 10px;color:#334155;white-space:nowrap;vertical-align:top;">{{.BankDisplay}}</td>{{end}}
          <td style="padding:8px 10px;color:#0f172a;vertical-align:top;">
            <div>{{.Description}}</div>
            {{if or .Installment .SplitNote}}
            <div style="font-size:12px;color:#64748b;margin-top:2px;">{{if .Installment}}Parcela {{.Installment}}{{end}}{{if and .Installment .SplitNote}} · {{end}}{{.SplitNote}}</div>
            {{end}}
          </td>
          <td align="right" class="tabular-nums" style="padding:8px 10px;text-align:right;white-space:nowrap;vertical-align:top;font-variant-numeric:tabular-nums;color:{{if .IsNegative}}#475569{{else}}#0f172a{{end}};">{{.AmountBRL}}</td>
        </tr>
        {{end}}
      {{else}}
        <tr style="border-bottom:1px solid #e2e8f0;">
          <td colspan="{{if .ShowBankColumn}}4{{else}}3{{end}}" style="padding:14px 10px;color:#64748b;text-align:center;">Nenhum lançamento no período.</td>
        </tr>
      {{end}}
    </tbody>
  </table>

  <table role="presentation" cellpadding="0" cellspacing="0" style="width:100%;border-collapse:collapse;margin-top:20px;font-size:13px;">
    {{if .SingleBank}}
    <tr>
      <td style="padding:10px 10px 4px 10px;text-align:right;font-weight:600;color:#0f172a;border-top:2px solid #0f172a;">Subtotal ({{.ScopeLabel}})</td>
      <td class="tabular-nums" style="padding:10px 10px 4px 10px;width:150px;text-align:right;font-weight:700;font-size:15px;color:#0f172a;border-top:2px solid #0f172a;font-variant-numeric:tabular-nums;white-space:nowrap;">{{.SubtotalBRL}}</td>
    </tr>
    {{else}}
    <tr>
      <td style="padding:6px 10px;text-align:right;color:#475569;">Subtotal do período</td>
      <td class="tabular-nums" style="padding:6px 10px;width:150px;text-align:right;color:#0f172a;font-variant-numeric:tabular-nums;white-space:nowrap;">{{.SubtotalBRL}}</td>
    </tr>
    <tr>
      <td style="padding:6px 10px;text-align:right;color:#475569;">Pagamentos abatidos</td>
      <td class="tabular-nums" style="padding:6px 10px;width:150px;text-align:right;color:#0f172a;font-variant-numeric:tabular-nums;white-space:nowrap;">{{.PaidBRL}}</td>
    </tr>
    {{if .ShowPriorBalance}}
    <tr>
      <td style="padding:6px 10px;text-align:right;color:#475569;">Saldo anterior</td>
      <td class="tabular-nums" style="padding:6px 10px;width:150px;text-align:right;color:#0f172a;font-variant-numeric:tabular-nums;white-space:nowrap;">{{.PriorBalanceBRL}}</td>
    </tr>
    {{end}}
    <tr>
      <td style="padding:10px 10px 4px 10px;text-align:right;font-weight:600;color:#0f172a;border-top:2px solid #0f172a;">Saldo final</td>
      <td class="tabular-nums" style="padding:10px 10px 4px 10px;width:150px;text-align:right;font-weight:700;font-size:15px;color:#0f172a;border-top:2px solid #0f172a;font-variant-numeric:tabular-nums;white-space:nowrap;">{{.TotalBRL}}</td>
    </tr>
    {{end}}
  </table>
</div>
</body>
</html>`))

func formatBankWithCard(bank, cardLabel string) string {
	name := domain.BankDisplayName(bank)
	card := strings.TrimSpace(cardLabel)
	if card == "" {
		return name
	}
	return fmt.Sprintf("%s (%s)", name, card)
}

func formatPaidDeduction(paidCents int64) string {
	if paidCents > 0 {
		return domain.FormatBRL(-paidCents)
	}
	return domain.FormatBRL(0)
}

func RenderHTML(data domain.ReceiptPreview) (string, error) {
	bankFilter := strings.TrimSpace(data.BankFilter)
	singleBank := bankFilter != ""

	scopeLabel := "Todos os bancos"
	if singleBank {
		scopeLabel = domain.BankDisplayName(bankFilter)
	}

	subject := strings.TrimSpace(data.Subject)
	if subject == "" {
		subject = BuildSubject(data)
	}

	items := make([]htmlReceiptItem, 0, len(data.Items))
	for _, it := range data.Items {
		splitNote := ""
		if it.IsSplit {
			splitNote = fmt.Sprintf("Parte de %s", domain.FormatBRL(it.TransactionTotalCents))
		}
		items = append(items, htmlReceiptItem{
			Date:        formatDateBR(it.PurchaseDate),
			BankDisplay: formatBankWithCard(it.Bank, it.CardLabel),
			Description: it.Description,
			Installment: formatInstallment(it.InstallmentCurrent, it.InstallmentTotal),
			SplitNote:   splitNote,
			AmountBRL:   domain.FormatBRL(it.PersonAmountCents),
			IsNegative:  it.PersonAmountCents < 0,
		})
	}

	view := htmlReceiptView{
		Subject:          subject,
		PersonName:       data.PersonName,
		PeriodLabel:      formatPeriodPT(data.ReferenceYear, data.ReferenceMonth),
		ScopeLabel:       scopeLabel,
		SingleBank:       singleBank,
		ShowBankColumn:   !singleBank,
		Items:            items,
		SubtotalBRL:      domain.FormatBRL(data.SubtotalCents),
		PaidBRL:          formatPaidDeduction(data.PaidCents),
		ShowPriorBalance: !singleBank && data.IncludePriorBalance,
		PriorBalanceBRL:  domain.FormatBRL(data.PriorBalanceCents),
		TotalBRL:         domain.FormatBRL(data.TotalCents),
	}

	var buf bytes.Buffer
	if err := receiptHTMLTemplate.Execute(&buf, view); err != nil {
		return "", fmt.Errorf("falha ao renderizar HTML do recibo: %w", err)
	}
	return buf.String(), nil
}

func RenderWhatsAppText(data domain.ReceiptPreview) string {
	bankFilter := strings.TrimSpace(data.BankFilter)
	singleBank := bankFilter != ""

	var b strings.Builder
	b.WriteString("Resumo de despesas")
	if name := sanitizeSingleLine(data.PersonName); name != "" {
		b.WriteString(" - ")
		b.WriteString(name)
	}
	b.WriteString("\nPeríodo: ")
	b.WriteString(formatPeriodPT(data.ReferenceYear, data.ReferenceMonth))
	if singleBank {
		b.WriteString("\nBanco: ")
		b.WriteString(domain.BankDisplayName(bankFilter))
	} else {
		b.WriteString("\nEscopo: Todos os bancos")
	}

	b.WriteString("\n\nLançamentos:\n")
	if len(data.Items) == 0 {
		b.WriteString("Nenhum lançamento no período.\n")
	} else {
		for _, it := range data.Items {
			b.WriteString("- ")
			b.WriteString(formatDateBR(it.PurchaseDate))
			if !singleBank {
				b.WriteString(" | ")
				b.WriteString(formatBankWithCard(it.Bank, it.CardLabel))
			}
			b.WriteString(" | ")
			b.WriteString(sanitizeSingleLine(it.Description))
			if inst := formatInstallment(it.InstallmentCurrent, it.InstallmentTotal); inst != "" {
				b.WriteString(" (Parcela ")
				b.WriteString(inst)
				b.WriteString(")")
			}
			if it.IsSplit {
				b.WriteString(" [Parte de ")
				b.WriteString(domain.FormatBRL(it.TransactionTotalCents))
				b.WriteString("]")
			}
			b.WriteString(" | ")
			b.WriteString(domain.FormatBRL(it.PersonAmountCents))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if singleBank {
		b.WriteString("Subtotal (")
		b.WriteString(domain.BankDisplayName(bankFilter))
		b.WriteString("): ")
		b.WriteString(domain.FormatBRL(data.SubtotalCents))
	} else {
		b.WriteString("Subtotal do período: ")
		b.WriteString(domain.FormatBRL(data.SubtotalCents))
		b.WriteString("\nPagamentos abatidos: ")
		b.WriteString(formatPaidDeduction(data.PaidCents))
		if data.IncludePriorBalance {
			b.WriteString("\nSaldo anterior: ")
			b.WriteString(domain.FormatBRL(data.PriorBalanceCents))
		}
		b.WriteString("\nSaldo final: ")
		b.WriteString(domain.FormatBRL(data.TotalCents))
	}

	return b.String()
}
