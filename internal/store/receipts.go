package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
)

type ReceiptQuery struct {
	PersonID            int64
	ReferenceYear       int
	ReferenceMonth      int
	BankFilter          string
	IncludePriorBalance bool
}

func (s *Store) GetReceiptAvailability(ctx context.Context, personID int64, year int, bankFilter string) ([]domain.ReceiptMonthAvailability, error) {
	if personID <= 0 {
		return nil, fmt.Errorf("%w: person_id invalido", ErrValidation)
	}
	if year <= 0 {
		return nil, fmt.Errorf("%w: year invalido", ErrValidation)
	}

	conn := s.DB()
	if _, err := getPersonRow(ctx, conn, personID); err != nil {
		return nil, err
	}

	months := make([]domain.ReceiptMonthAvailability, 12)
	for i := 0; i < 12; i++ {
		months[i] = domain.ReceiptMonthAvailability{Month: i + 1}
	}

	normalizedBank := ""
	if strings.TrimSpace(bankFilter) != "" {
		normalizedBank = domain.NormalizeBank(bankFilter)
	}

	query := `
		SELECT
			s.reference_month,
			COALESCE(SUM(ts.amount_cents), 0),
			COUNT(1)
		FROM transaction_splits ts
		JOIN transactions t ON t.id = ts.transaction_id
		JOIN statements s ON s.id = t.statement_id
		WHERE ts.person_id = ?
		  AND s.reference_year = ?
		  AND t.is_ignored = 0
	`
	args := []any{personID, year}
	if normalizedBank != "" {
		query += ` AND s.bank = ?`
		args = append(args, normalizedBank)
	}
	query += ` GROUP BY s.reference_month`

	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("consultar disponibilidade mensal de recibos: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var m, itemCount int
		var totalCents int64
		if err := rows.Scan(&m, &totalCents, &itemCount); err != nil {
			return nil, fmt.Errorf("ler disponibilidade mensal de recibos: %w", err)
		}
		if m >= 1 && m <= 12 {
			months[m-1].TotalCents = totalCents
			months[m-1].ItemCount = itemCount
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar disponibilidade mensal de recibos: %w", err)
	}
	return months, nil
}

func (s *Store) ComputeReceiptData(ctx context.Context, q ReceiptQuery) (domain.ReceiptPreview, error) {
	if q.PersonID <= 0 {
		return domain.ReceiptPreview{}, fmt.Errorf("%w: person_id invalido", ErrValidation)
	}
	if q.ReferenceYear <= 0 || q.ReferenceMonth < 1 || q.ReferenceMonth > 12 {
		return domain.ReceiptPreview{}, fmt.Errorf("%w: periodo de referencia invalido", ErrValidation)
	}

	conn := s.DB()
	person, err := getPersonRow(ctx, conn, q.PersonID)
	if err != nil {
		return domain.ReceiptPreview{}, err
	}

	bankFilter := ""
	if strings.TrimSpace(q.BankFilter) != "" {
		bankFilter = domain.NormalizeBank(q.BankFilter)
	}

	query := `
		SELECT
			t.purchase_date,
			s.bank,
			s.card_label,
			t.description,
			t.installment_current,
			t.installment_total,
			t.amount_cents,
			ts.amount_cents,
			(SELECT COUNT(1) FROM transaction_splits ts2 WHERE ts2.transaction_id = t.id) AS split_count
		FROM transaction_splits ts
		JOIN transactions t ON t.id = ts.transaction_id
		JOIN statements s ON s.id = t.statement_id
		WHERE ts.person_id = ?
		  AND s.reference_year = ?
		  AND s.reference_month = ?
		  AND t.is_ignored = 0
	`
	args := []any{q.PersonID, q.ReferenceYear, q.ReferenceMonth}
	if bankFilter != "" {
		query += ` AND s.bank = ?`
		args = append(args, bankFilter)
	}
	query += ` ORDER BY t.purchase_date ASC, s.bank ASC, t.line_index ASC, t.id ASC`

	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return domain.ReceiptPreview{}, fmt.Errorf("consultar itens do recibo: %w", err)
	}

	items := make([]domain.ReceiptItem, 0)
	var subtotalCents int64
	for rows.Next() {
		var item domain.ReceiptItem
		var instCur, instTot sql.NullInt64
		var splitCount int
		if err := rows.Scan(
			&item.PurchaseDate,
			&item.Bank,
			&item.CardLabel,
			&item.Description,
			&instCur,
			&instTot,
			&item.TransactionTotalCents,
			&item.PersonAmountCents,
			&splitCount,
		); err != nil {
			_ = rows.Close()
			return domain.ReceiptPreview{}, fmt.Errorf("ler item do recibo: %w", err)
		}
		item.InstallmentCurrent = nullIntPtr(instCur)
		item.InstallmentTotal = nullIntPtr(instTot)
		item.IsSplit = splitCount > 1
		subtotalCents += item.PersonAmountCents
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return domain.ReceiptPreview{}, fmt.Errorf("iterar itens do recibo: %w", err)
	}
	_ = rows.Close()

	var paidCents int64
	var priorBalanceCents int64
	includePrior := false
	totalCents := subtotalCents

	if bankFilter == "" {
		includePrior = q.IncludePriorBalance
		if err := conn.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(amount_cents), 0)
			FROM payments
			WHERE person_id = ? AND reference_year = ? AND reference_month = ?
		`, q.PersonID, q.ReferenceYear, q.ReferenceMonth).Scan(&paidCents); err != nil {
			return domain.ReceiptPreview{}, fmt.Errorf("consultar pagamentos do mes para recibo: %w", err)
		}

		if includePrior {
			var priorSpent, priorPaid int64
			if err := conn.QueryRowContext(ctx, `
				SELECT COALESCE(SUM(ts.amount_cents), 0)
				FROM transaction_splits ts
				JOIN transactions t ON t.id = ts.transaction_id
				JOIN statements s ON s.id = t.statement_id
				WHERE ts.person_id = ?
				  AND t.is_ignored = 0
				  AND (s.reference_year < ? OR (s.reference_year = ? AND s.reference_month < ?))
			`, q.PersonID, q.ReferenceYear, q.ReferenceYear, q.ReferenceMonth).Scan(&priorSpent); err != nil {
				return domain.ReceiptPreview{}, fmt.Errorf("consultar gastos anteriores para recibo: %w", err)
			}
			if err := conn.QueryRowContext(ctx, `
				SELECT COALESCE(SUM(amount_cents), 0)
				FROM payments
				WHERE person_id = ?
				  AND (reference_year < ? OR (reference_year = ? AND reference_month < ?))
			`, q.PersonID, q.ReferenceYear, q.ReferenceYear, q.ReferenceMonth).Scan(&priorPaid); err != nil {
				return domain.ReceiptPreview{}, fmt.Errorf("consultar pagamentos anteriores para recibo: %w", err)
			}
			priorBalanceCents = priorSpent - priorPaid
		}

		totalCents = subtotalCents - paidCents + priorBalanceCents
	}

	return domain.ReceiptPreview{
		PersonID:            person.ID,
		PersonName:          person.Name,
		PersonEmail:         person.Email,
		ReferenceYear:       q.ReferenceYear,
		ReferenceMonth:      q.ReferenceMonth,
		BankFilter:          bankFilter,
		IncludePriorBalance: includePrior,
		Items:               items,
		SubtotalCents:       subtotalCents,
		PaidCents:           paidCents,
		PriorBalanceCents:   priorBalanceCents,
		TotalCents:          totalCents,
	}, nil
}

func (s *Store) SaveReceipt(ctx context.Context, r domain.Receipt) (domain.Receipt, error) {
	if r.PersonID <= 0 {
		return domain.Receipt{}, fmt.Errorf("%w: person_id invalido", ErrValidation)
	}
	if r.ReferenceYear <= 0 || r.ReferenceMonth < 1 || r.ReferenceMonth > 12 {
		return domain.Receipt{}, fmt.Errorf("%w: periodo de referencia invalido", ErrValidation)
	}
	if strings.TrimSpace(r.Subject) == "" {
		return domain.Receipt{}, fmt.Errorf("%w: assunto obrigatorio", ErrValidation)
	}
	if strings.TrimSpace(r.DeliveryStatus) == "" {
		return domain.Receipt{}, fmt.Errorf("%w: status de entrega obrigatorio", ErrValidation)
	}

	bankFilter := ""
	if strings.TrimSpace(r.BankFilter) != "" {
		bankFilter = domain.NormalizeBank(r.BankFilter)
	}

	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return domain.Receipt{}, fmt.Errorf("iniciar transacao salvar recibo: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := getPersonRow(ctx, tx, r.PersonID); err != nil {
		return domain.Receipt{}, err
	}

	var sentAtParam any
	if r.SentAt != nil && strings.TrimSpace(*r.SentAt) != "" {
		sentAtParam = strings.TrimSpace(*r.SentAt)
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO receipts (
			person_id, reference_year, reference_month, bank_filter,
			include_prior_balance, recipient_email, subject, html_body,
			subtotal_cents, paid_cents, prior_balance_cents, total_cents,
			delivery_status, error_message, sent_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		r.PersonID,
		r.ReferenceYear,
		r.ReferenceMonth,
		bankFilter,
		boolToInt(r.IncludePriorBalance),
		strings.TrimSpace(r.RecipientEmail),
		strings.TrimSpace(r.Subject),
		r.HTMLBody,
		r.SubtotalCents,
		r.PaidCents,
		r.PriorBalanceCents,
		r.TotalCents,
		strings.TrimSpace(r.DeliveryStatus),
		strings.TrimSpace(r.ErrorMessage),
		sentAtParam,
	)
	if err != nil {
		return domain.Receipt{}, fmt.Errorf("inserir recibo: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Receipt{}, fmt.Errorf("obter id recibo: %w", err)
	}
	if err := touchMutationExec(ctx, tx); err != nil {
		return domain.Receipt{}, fmt.Errorf("atualizar mutacao: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Receipt{}, fmt.Errorf("confirmar recibo: %w", err)
	}

	return s.GetReceipt(ctx, id)
}

func (s *Store) ListReceipts(ctx context.Context) ([]domain.Receipt, error) {
	rows, err := s.DB().QueryContext(ctx, `
		SELECT
			r.id, r.person_id, p.name, r.reference_year, r.reference_month,
			r.bank_filter, r.include_prior_balance, r.recipient_email,
			r.subject, r.html_body, r.subtotal_cents, r.paid_cents,
			r.prior_balance_cents, r.total_cents, r.delivery_status,
			r.error_message, r.sent_at, r.created_at
		FROM receipts r
		JOIN people p ON p.id = r.person_id
		ORDER BY r.created_at DESC, r.id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("listar recibos: %w", err)
	}
	defer rows.Close()

	receipts := make([]domain.Receipt, 0)
	for rows.Next() {
		rec, err := scanReceiptRow(rows)
		if err != nil {
			return nil, fmt.Errorf("ler recibo: %w", err)
		}
		receipts = append(receipts, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar recibos: %w", err)
	}
	return receipts, nil
}

func (s *Store) GetReceipt(ctx context.Context, id int64) (domain.Receipt, error) {
	rec, err := scanReceiptRow(s.DB().QueryRowContext(ctx, `
		SELECT
			r.id, r.person_id, p.name, r.reference_year, r.reference_month,
			r.bank_filter, r.include_prior_balance, r.recipient_email,
			r.subject, r.html_body, r.subtotal_cents, r.paid_cents,
			r.prior_balance_cents, r.total_cents, r.delivery_status,
			r.error_message, r.sent_at, r.created_at
		FROM receipts r
		JOIN people p ON p.id = r.person_id
		WHERE r.id = ?
	`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Receipt{}, fmt.Errorf("%w: recibo %d", ErrNotFound, id)
	}
	if err != nil {
		return domain.Receipt{}, fmt.Errorf("buscar recibo %d: %w", id, err)
	}
	return rec, nil
}

func scanReceiptRow(sc rowScanner) (domain.Receipt, error) {
	var r domain.Receipt
	var includePrior int
	var sentAt sql.NullString
	err := sc.Scan(
		&r.ID,
		&r.PersonID,
		&r.PersonName,
		&r.ReferenceYear,
		&r.ReferenceMonth,
		&r.BankFilter,
		&includePrior,
		&r.RecipientEmail,
		&r.Subject,
		&r.HTMLBody,
		&r.SubtotalCents,
		&r.PaidCents,
		&r.PriorBalanceCents,
		&r.TotalCents,
		&r.DeliveryStatus,
		&r.ErrorMessage,
		&sentAt,
		&r.CreatedAt,
	)
	if err != nil {
		return domain.Receipt{}, err
	}
	r.IncludePriorBalance = intToBool(includePrior)
	r.SentAt = nullStringPtr(sentAt)
	return r, nil
}
