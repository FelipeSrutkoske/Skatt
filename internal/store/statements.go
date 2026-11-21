package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
)

type CreateStatementInput struct {
	Bank               string
	CardLabel          string
	ReferenceYear      int
	ReferenceMonth     int
	OriginalFilename   string
	StoredPath         string
	FileSHA256         string
	FileFormat         string
	ParserUsed         string
	DeclaredTotalCents *int64
	ReplaceStatementID *int64
	Transactions       []domain.PreviewTransaction
}

const statementSummarySelect = `
	SELECT
		s.id,
		s.bank,
		s.card_label,
		s.reference_year,
		s.reference_month,
		s.original_filename,
		s.stored_path,
		s.file_sha256,
		s.file_format,
		s.parser_used,
		s.declared_total_cents,
		s.imported_at,
		COALESCE((
			SELECT SUM(t.amount_cents)
			FROM transactions t
			WHERE t.statement_id = s.id AND t.is_ignored = 0
		), 0) AS total_cents,
		COALESCE((
			SELECT COUNT(1)
			FROM transactions t
			WHERE t.statement_id = s.id AND t.is_ignored = 0
		), 0) AS transaction_count,
		COALESCE((
			SELECT COUNT(1)
			FROM transactions t
			WHERE t.statement_id = s.id AND t.is_ignored = 0
			  AND EXISTS (SELECT 1 FROM transaction_splits ts WHERE ts.transaction_id = t.id)
		), 0) AS classified_count,
		COALESCE((
			SELECT COUNT(1)
			FROM transactions t
			WHERE t.statement_id = s.id AND t.is_ignored = 0
			  AND NOT EXISTS (SELECT 1 FROM transaction_splits ts WHERE ts.transaction_id = t.id)
		), 0) AS unassigned_count
	FROM statements s
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanStatementSummary(sc rowScanner) (domain.Statement, error) {
	var st domain.Statement
	var declared sql.NullInt64
	err := sc.Scan(
		&st.ID,
		&st.Bank,
		&st.CardLabel,
		&st.ReferenceYear,
		&st.ReferenceMonth,
		&st.OriginalFilename,
		&st.StoredPath,
		&st.FileSHA256,
		&st.FileFormat,
		&st.ParserUsed,
		&declared,
		&st.ImportedAt,
		&st.TotalCents,
		&st.TransactionCount,
		&st.ClassifiedCount,
		&st.UnassignedCount,
	)
	if err != nil {
		return domain.Statement{}, err
	}
	st.DeclaredTotalCents = nullInt64Ptr(declared)
	return st, nil
}

func (s *Store) FindDuplicates(ctx context.Context, sha256, bank string, year, month int) (*domain.DuplicateWarning, error) {
	conn := s.DB()
	sha256 = strings.TrimSpace(sha256)
	normalizedBank := ""
	if strings.TrimSpace(bank) != "" {
		normalizedBank = domain.NormalizeBank(bank)
	}

	var exactMatch *domain.Statement
	if sha256 != "" {
		st, err := scanStatementSummary(conn.QueryRowContext(
			ctx,
			statementSummarySelect+` WHERE s.file_sha256 = ? ORDER BY s.id DESC LIMIT 1`,
			sha256,
		))
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("verificar duplicata sha256: %w", err)
		}
		if err == nil {
			exactMatch = &st
		}
	}

	periodMatches := make([]domain.Statement, 0)
	if normalizedBank != "" && year > 0 && month >= 1 && month <= 12 {
		rows, err := conn.QueryContext(
			ctx,
			statementSummarySelect+` WHERE s.reference_year = ? AND s.reference_month = ? AND s.bank = ? ORDER BY s.id DESC`,
			year, month, normalizedBank,
		)
		if err != nil {
			return nil, fmt.Errorf("verificar duplicata periodo banco: %w", err)
		}
		for rows.Next() {
			st, err := scanStatementSummary(rows)
			if err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("ler fatura duplicada: %w", err)
			}
			periodMatches = append(periodMatches, st)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("iterar faturas duplicadas: %w", err)
		}
		_ = rows.Close()
	}

	if exactMatch == nil && len(periodMatches) == 0 {
		return nil, nil
	}

	return &domain.DuplicateWarning{
		ExactFileMatch:    exactMatch,
		PeriodBankMatches: periodMatches,
	}, nil
}

func (s *Store) CreateStatement(ctx context.Context, in CreateStatementInput) (domain.Statement, string, error) {
	if in.ReferenceYear <= 0 || in.ReferenceMonth < 1 || in.ReferenceMonth > 12 {
		return domain.Statement{}, "", fmt.Errorf("%w: periodo de referencia invalido", ErrValidation)
	}

	bank := domain.NormalizeBank(in.Bank)
	cardLabel := strings.TrimSpace(in.CardLabel)
	origFilename := strings.TrimSpace(in.OriginalFilename)
	storedPath := strings.TrimSpace(in.StoredPath)
	fileSHA := strings.TrimSpace(in.FileSHA256)
	fileFormat := strings.TrimSpace(in.FileFormat)
	if fileFormat == "" {
		fileFormat = "manual"
	}
	parserUsed := strings.TrimSpace(in.ParserUsed)
	if parserUsed == "" {
		parserUsed = "manual"
	}

	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return domain.Statement{}, "", fmt.Errorf("iniciar transacao criar fatura: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var oldStoredPath string
	if in.ReplaceStatementID != nil {
		err := tx.QueryRowContext(ctx, `SELECT stored_path FROM statements WHERE id = ?`, *in.ReplaceStatementID).Scan(&oldStoredPath)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Statement{}, "", fmt.Errorf("%w: fatura %d", ErrNotFound, *in.ReplaceStatementID)
		}
		if err != nil {
			return domain.Statement{}, "", fmt.Errorf("buscar fatura a substituir: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM statements WHERE id = ?`, *in.ReplaceStatementID); err != nil {
			return domain.Statement{}, "", fmt.Errorf("remover fatura substituida: %w", err)
		}
	}

	var declared any
	if in.DeclaredTotalCents != nil {
		declared = *in.DeclaredTotalCents
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO statements (
			bank, card_label, reference_year, reference_month,
			original_filename, stored_path, file_sha256,
			file_format, parser_used, declared_total_cents
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, bank, cardLabel, in.ReferenceYear, in.ReferenceMonth, origFilename, storedPath, fileSHA, fileFormat, parserUsed, declared)
	if err != nil {
		return domain.Statement{}, "", fmt.Errorf("inserir fatura: %w", err)
	}
	stmtID, err := res.LastInsertId()
	if err != nil {
		return domain.Statement{}, "", fmt.Errorf("obter id fatura: %w", err)
	}

	for idx, item := range in.Transactions {
		purchaseDate := strings.TrimSpace(item.PurchaseDate)
		description := strings.TrimSpace(item.Description)
		if purchaseDate == "" || description == "" {
			return domain.Statement{}, "", fmt.Errorf("%w: transacao %d sem data ou descricao", ErrValidation, idx)
		}
		normMerchant := strings.TrimSpace(item.NormalizedMerchant)
		if normMerchant == "" {
			normMerchant = strings.ToUpper(description)
		}

		suggestedID := item.SuggestedPersonID
		if suggestedID == nil && normMerchant != "" {
			foundID, err := lookupSuggestedPersonRow(ctx, tx, normMerchant)
			if err != nil {
				return domain.Statement{}, "", err
			}
			suggestedID = foundID
		}

		var instCur, instTot, sugParam any
		if item.InstallmentCurrent != nil {
			instCur = *item.InstallmentCurrent
		}
		if item.InstallmentTotal != nil {
			instTot = *item.InstallmentTotal
		}
		if suggestedID != nil {
			sugParam = *suggestedID
		}

		txRes, err := tx.ExecContext(ctx, `
			INSERT INTO transactions (
				statement_id, line_index, purchase_date, description,
				normalized_merchant, installment_current, installment_total,
				amount_cents, is_ignored, suggested_person_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, stmtID, idx, purchaseDate, description, normMerchant, instCur, instTot, item.AmountCents, boolToInt(item.IsIgnored), sugParam)
		if err != nil {
			return domain.Statement{}, "", fmt.Errorf("inserir transacao %d: %w", idx, err)
		}
		txID, err := txRes.LastInsertId()
		if err != nil {
			return domain.Statement{}, "", fmt.Errorf("obter id transacao %d: %w", idx, err)
		}

		if len(item.Splits) > 0 {
			if err := validateSplits(item.AmountCents, item.Splits); err != nil {
				return domain.Statement{}, "", err
			}
			if err := verifyPeopleExistTx(ctx, tx, item.Splits); err != nil {
				return domain.Statement{}, "", err
			}
			for _, sp := range item.Splits {
				if _, err := tx.ExecContext(
					ctx,
					`INSERT INTO transaction_splits (transaction_id, person_id, amount_cents) VALUES (?, ?, ?)`,
					txID, sp.PersonID, sp.AmountCents,
				); err != nil {
					return domain.Statement{}, "", fmt.Errorf("inserir split transacao %d: %w", idx, err)
				}
			}
			if len(item.Splits) == 1 && !item.IsIgnored && normMerchant != "" {
				if err := upsertAssignmentRuleExec(ctx, tx, normMerchant, item.Splits[0].PersonID); err != nil {
					return domain.Statement{}, "", fmt.Errorf("atualizar regra atribuicao: %w", err)
				}
			}
		}
	}

	if err := touchMutationExec(ctx, tx); err != nil {
		return domain.Statement{}, "", fmt.Errorf("atualizar mutacao: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Statement{}, "", fmt.Errorf("confirmar criacao fatura: %w", err)
	}

	created, err := s.GetStatement(ctx, stmtID)
	if err != nil {
		return domain.Statement{}, oldStoredPath, err
	}
	return created, oldStoredPath, nil
}

func (s *Store) UpdateStatementStoredPath(ctx context.Context, id int64, storedPath string) error {
	res, err := s.DB().ExecContext(ctx, `UPDATE statements SET stored_path = ? WHERE id = ?`, strings.TrimSpace(storedPath), id)
	if err != nil {
		return fmt.Errorf("atualizar stored_path fatura: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("verificar atualizacao stored_path: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: fatura %d", ErrNotFound, id)
	}
	return nil
}

func (s *Store) GetStatement(ctx context.Context, id int64) (domain.Statement, error) {
	st, err := scanStatementSummary(s.DB().QueryRowContext(ctx, statementSummarySelect+` WHERE s.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Statement{}, fmt.Errorf("%w: fatura %d", ErrNotFound, id)
	}
	if err != nil {
		return domain.Statement{}, fmt.Errorf("buscar fatura %d: %w", id, err)
	}
	return st, nil
}

func (s *Store) GetStatementDetail(ctx context.Context, id int64) (domain.StatementDetail, error) {
	conn := s.DB()
	stmt, err := s.GetStatement(ctx, id)
	if err != nil {
		return domain.StatementDetail{}, err
	}

	txRows, err := conn.QueryContext(ctx, `
		SELECT
			id, statement_id, line_index, purchase_date, description,
			normalized_merchant, installment_current, installment_total,
			amount_cents, is_ignored, suggested_person_id, created_at
		FROM transactions
		WHERE statement_id = ?
		ORDER BY line_index ASC, id ASC
	`, id)
	if err != nil {
		return domain.StatementDetail{}, fmt.Errorf("listar transacoes fatura: %w", err)
	}

	txs := make([]domain.Transaction, 0)
	txIndex := make(map[int64]int)
	for txRows.Next() {
		var t domain.Transaction
		var instCur, instTot, sugID sql.NullInt64
		var isIgnored int
		if err := txRows.Scan(
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
		); err != nil {
			_ = txRows.Close()
			return domain.StatementDetail{}, fmt.Errorf("ler transacao: %w", err)
		}
		t.InstallmentCurrent = nullIntPtr(instCur)
		t.InstallmentTotal = nullIntPtr(instTot)
		t.IsIgnored = intToBool(isIgnored)
		t.SuggestedPersonID = nullInt64Ptr(sugID)
		t.Splits = make([]domain.TransactionSplit, 0)
		txIndex[t.ID] = len(txs)
		txs = append(txs, t)
	}
	if err := txRows.Err(); err != nil {
		_ = txRows.Close()
		return domain.StatementDetail{}, fmt.Errorf("iterar transacoes: %w", err)
	}
	_ = txRows.Close()

	splitRows, err := conn.QueryContext(ctx, `
		SELECT ts.id, ts.transaction_id, ts.person_id, p.name, ts.amount_cents
		FROM transaction_splits ts
		JOIN transactions t ON t.id = ts.transaction_id
		JOIN people p ON p.id = ts.person_id
		WHERE t.statement_id = ?
		ORDER BY ts.id ASC
	`, id)
	if err != nil {
		return domain.StatementDetail{}, fmt.Errorf("listar splits fatura: %w", err)
	}
	for splitRows.Next() {
		var sp domain.TransactionSplit
		if err := splitRows.Scan(&sp.ID, &sp.TransactionID, &sp.PersonID, &sp.PersonName, &sp.AmountCents); err != nil {
			_ = splitRows.Close()
			return domain.StatementDetail{}, fmt.Errorf("ler split fatura: %w", err)
		}
		if idx, ok := txIndex[sp.TransactionID]; ok {
			txs[idx].Splits = append(txs[idx].Splits, sp)
		}
	}
	if err := splitRows.Err(); err != nil {
		_ = splitRows.Close()
		return domain.StatementDetail{}, fmt.Errorf("iterar splits fatura: %w", err)
	}
	_ = splitRows.Close()

	totalsRows, err := conn.QueryContext(ctx, `
		SELECT p.id, p.name, p.is_self, p.color, COALESCE(SUM(ts.amount_cents), 0)
		FROM transaction_splits ts
		JOIN transactions t ON t.id = ts.transaction_id
		JOIN people p ON p.id = ts.person_id
		WHERE t.statement_id = ? AND t.is_ignored = 0
		GROUP BY p.id, p.name, p.is_self, p.color
		ORDER BY p.is_self DESC, p.name ASC
	`, id)
	if err != nil {
		return domain.StatementDetail{}, fmt.Errorf("calcular totais por pessoa na fatura: %w", err)
	}
	totalsByPerson := make([]domain.PersonTotal, 0)
	for totalsRows.Next() {
		var pt domain.PersonTotal
		var isSelf int
		if err := totalsRows.Scan(&pt.PersonID, &pt.PersonName, &isSelf, &pt.Color, &pt.TotalCents); err != nil {
			_ = totalsRows.Close()
			return domain.StatementDetail{}, fmt.Errorf("ler total por pessoa na fatura: %w", err)
		}
		pt.IsSelf = intToBool(isSelf)
		totalsByPerson = append(totalsByPerson, pt)
	}
	if err := totalsRows.Err(); err != nil {
		_ = totalsRows.Close()
		return domain.StatementDetail{}, fmt.Errorf("iterar totais por pessoa: %w", err)
	}
	_ = totalsRows.Close()

	var unassignedTotal int64
	for _, t := range txs {
		if !t.IsIgnored && len(t.Splits) == 0 {
			unassignedTotal += t.AmountCents
		}
	}

	return domain.StatementDetail{
		Statement:            stmt,
		Transactions:         txs,
		TotalsByPerson:       totalsByPerson,
		UnassignedTotalCents: unassignedTotal,
	}, nil
}

func (s *Store) DeleteStatement(ctx context.Context, id int64) (string, error) {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("iniciar transacao excluir fatura: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var storedPath string
	err = tx.QueryRowContext(ctx, `SELECT stored_path FROM statements WHERE id = ?`, id).Scan(&storedPath)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: fatura %d", ErrNotFound, id)
	}
	if err != nil {
		return "", fmt.Errorf("buscar fatura para exclusao: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM statements WHERE id = ?`, id); err != nil {
		return "", fmt.Errorf("excluir fatura: %w", err)
	}
	if err := touchMutationExec(ctx, tx); err != nil {
		return "", fmt.Errorf("atualizar mutacao: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("confirmar exclusao fatura: %w", err)
	}
	return storedPath, nil
}

func (s *Store) LookupSuggestedPerson(ctx context.Context, normalizedMerchant string) (*int64, error) {
	return lookupSuggestedPersonRow(ctx, s.DB(), normalizedMerchant)
}

func lookupSuggestedPersonRow(ctx context.Context, q queryRower, normalizedMerchant string) (*int64, error) {
	merchant := strings.TrimSpace(normalizedMerchant)
	if merchant == "" {
		return nil, nil
	}
	var personID int64
	err := q.QueryRowContext(ctx, `SELECT person_id FROM assignment_rules WHERE normalized_merchant = ?`, merchant).Scan(&personID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("consultar regra de sugestao: %w", err)
	}
	return &personID, nil
}

func upsertAssignmentRuleExec(ctx context.Context, exec execer, normalizedMerchant string, personID int64) error {
	_, err := exec.ExecContext(ctx, `
		INSERT INTO assignment_rules (normalized_merchant, person_id, match_count, updated_at)
		VALUES (?, ?, 1, datetime('now'))
		ON CONFLICT(normalized_merchant) DO UPDATE SET
			match_count = CASE
				WHEN assignment_rules.person_id = excluded.person_id THEN assignment_rules.match_count + 1
				ELSE 1
			END,
			person_id = excluded.person_id,
			updated_at = datetime('now')
	`, normalizedMerchant, personID)
	return err
}
