package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
)

type UpdateTransactionInput struct {
	PurchaseDate       *string
	Description        *string
	NormalizedMerchant *string
	AmountCents        *int64
	IsIgnored          *bool
	InstallmentCurrent *int
	InstallmentTotal   *int
	ClearInstallments  bool
}

func validateSplits(amountCents int64, splits []domain.SplitInput) error {
	if len(splits) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(splits))
	var sum int64
	for _, sp := range splits {
		if sp.PersonID <= 0 {
			return fmt.Errorf("%w: person_id invalido no split", ErrValidation)
		}
		if len(splits) > 1 && sp.AmountCents == 0 {
			return fmt.Errorf("%w: valor do split nao pode ser zero", ErrValidation)
		}
		if _, dup := seen[sp.PersonID]; dup {
			return fmt.Errorf("%w: pessoa %d duplicada no split", ErrValidation, sp.PersonID)
		}
		seen[sp.PersonID] = struct{}{}
		sum += sp.AmountCents
	}
	if sum != amountCents {
		return fmt.Errorf("%w: soma dos splits (%d) difere do valor da transacao (%d)", ErrValidation, sum, amountCents)
	}
	return nil
}

func verifyPeopleExistTx(ctx context.Context, tx *sql.Tx, splits []domain.SplitInput) error {
	for _, sp := range splits {
		var exists int
		err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM people WHERE id = ?`, sp.PersonID).Scan(&exists)
		if err != nil {
			return fmt.Errorf("verificar pessoa %d: %w", sp.PersonID, err)
		}
		if exists == 0 {
			return fmt.Errorf("%w: pessoa %d", ErrNotFound, sp.PersonID)
		}
	}
	return nil
}

func (s *Store) SetTransactionSplits(ctx context.Context, txID int64, splits []domain.SplitInput) (domain.Transaction, error) {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("iniciar transacao splits: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var amountCents int64
	var normalizedMerchant string
	var isIgnored int
	err = tx.QueryRowContext(
		ctx,
		`SELECT amount_cents, normalized_merchant, is_ignored FROM transactions WHERE id = ?`,
		txID,
	).Scan(&amountCents, &normalizedMerchant, &isIgnored)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Transaction{}, fmt.Errorf("%w: transacao %d", ErrNotFound, txID)
	}
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("buscar transacao %d: %w", txID, err)
	}

	if err := validateSplits(amountCents, splits); err != nil {
		return domain.Transaction{}, err
	}
	if err := verifyPeopleExistTx(ctx, tx, splits); err != nil {
		return domain.Transaction{}, err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM transaction_splits WHERE transaction_id = ?`, txID); err != nil {
		return domain.Transaction{}, fmt.Errorf("limpar splits anteriores: %w", err)
	}
	for _, sp := range splits {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO transaction_splits (transaction_id, person_id, amount_cents) VALUES (?, ?, ?)`,
			txID, sp.PersonID, sp.AmountCents,
		); err != nil {
			return domain.Transaction{}, fmt.Errorf("inserir split: %w", err)
		}
	}

	if len(splits) == 1 && !intToBool(isIgnored) && strings.TrimSpace(normalizedMerchant) != "" {
		if err := upsertAssignmentRuleExec(ctx, tx, normalizedMerchant, splits[0].PersonID); err != nil {
			return domain.Transaction{}, fmt.Errorf("salvar regra de atribuicao: %w", err)
		}
	}

	if err := touchMutationExec(ctx, tx); err != nil {
		return domain.Transaction{}, fmt.Errorf("atualizar mutacao: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Transaction{}, fmt.Errorf("confirmar splits: %w", err)
	}

	return s.getTransactionWithSplits(ctx, txID)
}

func (s *Store) UpdateTransaction(ctx context.Context, txID int64, in UpdateTransactionInput) (domain.Transaction, error) {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("iniciar transacao atualizar compra: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var current domain.Transaction
	var instCur, instTot, sugID sql.NullInt64
	var isIgnored int
	err = tx.QueryRowContext(ctx, `
		SELECT
			id, statement_id, line_index, purchase_date, description,
			normalized_merchant, installment_current, installment_total,
			amount_cents, is_ignored, suggested_person_id, created_at
		FROM transactions
		WHERE id = ?
	`, txID).Scan(
		&current.ID,
		&current.StatementID,
		&current.LineIndex,
		&current.PurchaseDate,
		&current.Description,
		&current.NormalizedMerchant,
		&instCur,
		&instTot,
		&current.AmountCents,
		&isIgnored,
		&sugID,
		&current.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Transaction{}, fmt.Errorf("%w: transacao %d", ErrNotFound, txID)
	}
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("buscar transacao %d: %w", txID, err)
	}
	current.InstallmentCurrent = nullIntPtr(instCur)
	current.InstallmentTotal = nullIntPtr(instTot)
	current.IsIgnored = intToBool(isIgnored)
	current.SuggestedPersonID = nullInt64Ptr(sugID)

	oldAmount := current.AmountCents

	if in.PurchaseDate != nil {
		current.PurchaseDate = strings.TrimSpace(*in.PurchaseDate)
	}
	if in.Description != nil {
		current.Description = strings.TrimSpace(*in.Description)
	}
	if in.NormalizedMerchant != nil {
		current.NormalizedMerchant = strings.TrimSpace(*in.NormalizedMerchant)
	}
	if in.AmountCents != nil {
		current.AmountCents = *in.AmountCents
	}
	if in.IsIgnored != nil {
		current.IsIgnored = *in.IsIgnored
	}
	if in.ClearInstallments {
		current.InstallmentCurrent = nil
		current.InstallmentTotal = nil
	} else {
		if in.InstallmentCurrent != nil {
			v := *in.InstallmentCurrent
			current.InstallmentCurrent = &v
		}
		if in.InstallmentTotal != nil {
			v := *in.InstallmentTotal
			current.InstallmentTotal = &v
		}
	}

	if current.PurchaseDate == "" || current.Description == "" {
		return domain.Transaction{}, fmt.Errorf("%w: data e descricao sao obrigatorias", ErrValidation)
	}
	if current.NormalizedMerchant == "" {
		current.NormalizedMerchant = strings.ToUpper(current.Description)
	}

	var curParam, totParam any
	if current.InstallmentCurrent != nil {
		curParam = *current.InstallmentCurrent
	}
	if current.InstallmentTotal != nil {
		totParam = *current.InstallmentTotal
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE transactions
		SET purchase_date = ?, description = ?, normalized_merchant = ?,
			installment_current = ?, installment_total = ?,
			amount_cents = ?, is_ignored = ?
		WHERE id = ?
	`, current.PurchaseDate, current.Description, current.NormalizedMerchant, curParam, totParam, current.AmountCents, boolToInt(current.IsIgnored), txID); err != nil {
		return domain.Transaction{}, fmt.Errorf("atualizar transacao: %w", err)
	}

	if current.AmountCents != oldAmount {
		var splitCount int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM transaction_splits WHERE transaction_id = ?`, txID).Scan(&splitCount); err != nil {
			return domain.Transaction{}, fmt.Errorf("contar splits na atualizacao: %w", err)
		}
		if splitCount == 1 {
			if _, err := tx.ExecContext(ctx, `UPDATE transaction_splits SET amount_cents = ? WHERE transaction_id = ?`, current.AmountCents, txID); err != nil {
				return domain.Transaction{}, fmt.Errorf("ajustar split unico: %w", err)
			}
		} else if splitCount > 1 {
			if _, err := tx.ExecContext(ctx, `DELETE FROM transaction_splits WHERE transaction_id = ?`, txID); err != nil {
				return domain.Transaction{}, fmt.Errorf("limpar splits divergentes: %w", err)
			}
		}
	}

	if err := touchMutationExec(ctx, tx); err != nil {
		return domain.Transaction{}, fmt.Errorf("atualizar mutacao: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Transaction{}, fmt.Errorf("confirmar atualizacao transacao: %w", err)
	}

	return s.getTransactionWithSplits(ctx, txID)
}

func (s *Store) ApplySuggestions(ctx context.Context, statementID int64) (int, error) {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("iniciar transacao aplicar sugestoes: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM statements WHERE id = ?`, statementID).Scan(&exists); err != nil {
		return 0, fmt.Errorf("verificar fatura: %w", err)
	}
	if exists == 0 {
		return 0, fmt.Errorf("%w: fatura %d", ErrNotFound, statementID)
	}

	type pendingSuggestion struct {
		txID       int64
		personID   int64
		amount     int64
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT t.id, t.suggested_person_id, t.amount_cents
		FROM transactions t
		WHERE t.statement_id = ?
		  AND t.is_ignored = 0
		  AND t.suggested_person_id IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM transaction_splits ts WHERE ts.transaction_id = t.id)
	`, statementID)
	if err != nil {
		return 0, fmt.Errorf("buscar sugestoes pendentes: %w", err)
	}
	items := make([]pendingSuggestion, 0)
	for rows.Next() {
		var item pendingSuggestion
		if err := rows.Scan(&item.txID, &item.personID, &item.amount); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("ler sugestao pendente: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, fmt.Errorf("iterar sugestoes pendentes: %w", err)
	}
	_ = rows.Close()

	for _, item := range items {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO transaction_splits (transaction_id, person_id, amount_cents) VALUES (?, ?, ?)`,
			item.txID, item.personID, item.amount,
		); err != nil {
			return 0, fmt.Errorf("aplicar sugestao transacao %d: %w", item.txID, err)
		}
	}

	if len(items) > 0 {
		if err := touchMutationExec(ctx, tx); err != nil {
			return 0, fmt.Errorf("atualizar mutacao: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("confirmar aplicacao sugestoes: %w", err)
	}
	return len(items), nil
}

func (s *Store) AssignRemaining(ctx context.Context, statementID, personID int64) (int, error) {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("iniciar transacao atribuir restantes: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var stmtExists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM statements WHERE id = ?`, statementID).Scan(&stmtExists); err != nil {
		return 0, fmt.Errorf("verificar fatura: %w", err)
	}
	if stmtExists == 0 {
		return 0, fmt.Errorf("%w: fatura %d", ErrNotFound, statementID)
	}
	if _, err := getPersonRow(ctx, tx, personID); err != nil {
		return 0, err
	}

	type pendingTx struct {
		txID   int64
		amount int64
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT t.id, t.amount_cents
		FROM transactions t
		WHERE t.statement_id = ?
		  AND t.is_ignored = 0
		  AND NOT EXISTS (SELECT 1 FROM transaction_splits ts WHERE ts.transaction_id = t.id)
	`, statementID)
	if err != nil {
		return 0, fmt.Errorf("buscar transacoes restantes: %w", err)
	}
	items := make([]pendingTx, 0)
	for rows.Next() {
		var item pendingTx
		if err := rows.Scan(&item.txID, &item.amount); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("ler transacao restante: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, fmt.Errorf("iterar transacoes restantes: %w", err)
	}
	_ = rows.Close()

	for _, item := range items {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO transaction_splits (transaction_id, person_id, amount_cents) VALUES (?, ?, ?)`,
			item.txID, personID, item.amount,
		); err != nil {
			return 0, fmt.Errorf("atribuir transacao restante %d: %w", item.txID, err)
		}
	}

	if len(items) > 0 {
		if err := touchMutationExec(ctx, tx); err != nil {
			return 0, fmt.Errorf("atualizar mutacao: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("confirmar atribuicao restantes: %w", err)
	}
	return len(items), nil
}

func (s *Store) getTransactionWithSplits(ctx context.Context, txID int64) (domain.Transaction, error) {
	conn := s.DB()
	var t domain.Transaction
	var instCur, instTot, sugID sql.NullInt64
	var isIgnored int
	err := conn.QueryRowContext(ctx, `
		SELECT
			id, statement_id, line_index, purchase_date, description,
			normalized_merchant, installment_current, installment_total,
			amount_cents, is_ignored, suggested_person_id, created_at
		FROM transactions
		WHERE id = ?
	`, txID).Scan(
		&t.ID,
		&t.StatementID,
		&t.LineIndex,
		&t.PurchaseDate,
		&t.Description,
		&t.NormalizedMerchant,
		&instCur,
		&instTot,
		&t.AmountCents,
		&isIgnored,
		&sugID,
		&t.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Transaction{}, fmt.Errorf("%w: transacao %d", ErrNotFound, txID)
	}
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("buscar transacao %d: %w", txID, err)
	}
	t.InstallmentCurrent = nullIntPtr(instCur)
	t.InstallmentTotal = nullIntPtr(instTot)
	t.IsIgnored = intToBool(isIgnored)
	t.SuggestedPersonID = nullInt64Ptr(sugID)
	t.Splits = make([]domain.TransactionSplit, 0)

	rows, err := conn.QueryContext(ctx, `
		SELECT ts.id, ts.transaction_id, ts.person_id, p.name, ts.amount_cents
		FROM transaction_splits ts
		JOIN people p ON p.id = ts.person_id
		WHERE ts.transaction_id = ?
		ORDER BY ts.id ASC
	`, txID)
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("listar splits transacao %d: %w", txID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var sp domain.TransactionSplit
		if err := rows.Scan(&sp.ID, &sp.TransactionID, &sp.PersonID, &sp.PersonName, &sp.AmountCents); err != nil {
			return domain.Transaction{}, fmt.Errorf("ler split transacao %d: %w", txID, err)
		}
		t.Splits = append(t.Splits, sp)
	}
	if err := rows.Err(); err != nil {
		return domain.Transaction{}, fmt.Errorf("iterar splits transacao %d: %w", txID, err)
	}
	return t, nil
}
