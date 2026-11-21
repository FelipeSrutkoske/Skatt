package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
)

func (s *Store) ListPeople(ctx context.Context, includeArchived bool) ([]domain.Person, error) {
	conn := s.DB()
	query := `SELECT id, name, email, is_self, color, archived, created_at FROM people`
	if !includeArchived {
		query += ` WHERE archived = 0`
	}
	query += ` ORDER BY is_self DESC, name ASC`

	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("listar pessoas: %w", err)
	}
	defer rows.Close()

	people := make([]domain.Person, 0)
	for rows.Next() {
		var p domain.Person
		var isSelf, archived int
		if err := rows.Scan(&p.ID, &p.Name, &p.Email, &isSelf, &p.Color, &archived, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("ler pessoa: %w", err)
		}
		p.IsSelf = intToBool(isSelf)
		p.Archived = intToBool(archived)
		people = append(people, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar pessoas: %w", err)
	}
	return people, nil
}

func (s *Store) GetPerson(ctx context.Context, id int64) (domain.Person, error) {
	return getPersonRow(ctx, s.DB(), id)
}

func getPersonRow(ctx context.Context, q queryRower, id int64) (domain.Person, error) {
	var p domain.Person
	var isSelf, archived int
	err := q.QueryRowContext(
		ctx,
		`SELECT id, name, email, is_self, color, archived, created_at FROM people WHERE id = ?`,
		id,
	).Scan(&p.ID, &p.Name, &p.Email, &isSelf, &p.Color, &archived, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Person{}, fmt.Errorf("%w: pessoa %d", ErrNotFound, id)
	}
	if err != nil {
		return domain.Person{}, fmt.Errorf("buscar pessoa %d: %w", id, err)
	}
	p.IsSelf = intToBool(isSelf)
	p.Archived = intToBool(archived)
	return p, nil
}

func (s *Store) CreatePerson(ctx context.Context, name, email, color string) (domain.Person, error) {
	name = strings.TrimSpace(name)
	email = strings.TrimSpace(email)
	color = strings.TrimSpace(color)
	if name == "" {
		return domain.Person{}, fmt.Errorf("%w: nome obrigatorio", ErrValidation)
	}
	if color == "" {
		color = "#475569"
	}

	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return domain.Person{}, fmt.Errorf("iniciar transacao criar pessoa: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(
		ctx,
		`INSERT INTO people (name, email, is_self, color, archived) VALUES (?, ?, 0, ?, 0)`,
		name, email, color,
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return domain.Person{}, fmt.Errorf("%w: ja existe pessoa com o nome %q", ErrConflict, name)
		}
		return domain.Person{}, fmt.Errorf("inserir pessoa: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Person{}, fmt.Errorf("obter id pessoa: %w", err)
	}
	if err := touchMutationExec(ctx, tx); err != nil {
		return domain.Person{}, fmt.Errorf("atualizar mutacao: %w", err)
	}
	p, err := getPersonRow(ctx, tx, id)
	if err != nil {
		return domain.Person{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Person{}, fmt.Errorf("confirmar criacao pessoa: %w", err)
	}
	return p, nil
}

func (s *Store) UpdatePerson(ctx context.Context, id int64, name, email, color string, archived bool) (domain.Person, error) {
	name = strings.TrimSpace(name)
	email = strings.TrimSpace(email)
	color = strings.TrimSpace(color)
	if name == "" {
		return domain.Person{}, fmt.Errorf("%w: nome obrigatorio", ErrValidation)
	}

	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return domain.Person{}, fmt.Errorf("iniciar transacao atualizar pessoa: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	existing, err := getPersonRow(ctx, tx, id)
	if err != nil {
		return domain.Person{}, err
	}
	if existing.IsSelf && archived {
		return domain.Person{}, fmt.Errorf("%w: nao e possivel arquivar a pessoa principal", ErrForbidden)
	}
	if color == "" {
		color = existing.Color
	}

	_, err = tx.ExecContext(
		ctx,
		`UPDATE people SET name = ?, email = ?, color = ?, archived = ? WHERE id = ?`,
		name, email, color, boolToInt(archived), id,
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return domain.Person{}, fmt.Errorf("%w: ja existe pessoa com o nome %q", ErrConflict, name)
		}
		return domain.Person{}, fmt.Errorf("atualizar pessoa: %w", err)
	}
	if err := touchMutationExec(ctx, tx); err != nil {
		return domain.Person{}, fmt.Errorf("atualizar mutacao: %w", err)
	}
	updated, err := getPersonRow(ctx, tx, id)
	if err != nil {
		return domain.Person{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Person{}, fmt.Errorf("confirmar atualizacao pessoa: %w", err)
	}
	return updated, nil
}

func (s *Store) DeletePerson(ctx context.Context, id int64) error {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("iniciar transacao excluir pessoa: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	person, err := getPersonRow(ctx, tx, id)
	if err != nil {
		return err
	}
	if person.IsSelf {
		return fmt.Errorf("%w: nao e possivel excluir a pessoa principal", ErrForbidden)
	}

	var splitCount, paymentCount, receiptCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM transaction_splits WHERE person_id = ?`, id).Scan(&splitCount); err != nil {
		return fmt.Errorf("verificar splits pessoa: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM payments WHERE person_id = ?`, id).Scan(&paymentCount); err != nil {
		return fmt.Errorf("verificar pagamentos pessoa: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM receipts WHERE person_id = ?`, id).Scan(&receiptCount); err != nil {
		return fmt.Errorf("verificar recibos pessoa: %w", err)
	}
	if splitCount > 0 || paymentCount > 0 || receiptCount > 0 {
		return fmt.Errorf("%w: pessoa possui historico vinculado; arquive em vez de excluir", ErrConflict)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM people WHERE id = ?`, id); err != nil {
		return fmt.Errorf("excluir pessoa: %w", err)
	}
	if err := touchMutationExec(ctx, tx); err != nil {
		return fmt.Errorf("atualizar mutacao: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("confirmar exclusao pessoa: %w", err)
	}
	return nil
}

func (s *Store) CreatePayment(ctx context.Context, in domain.PaymentInput) (domain.Payment, error) {
	if in.PersonID <= 0 {
		return domain.Payment{}, fmt.Errorf("%w: person_id invalido", ErrValidation)
	}
	if in.ReferenceYear <= 0 || in.ReferenceMonth < 1 || in.ReferenceMonth > 12 {
		return domain.Payment{}, fmt.Errorf("%w: periodo de referencia invalido", ErrValidation)
	}
	if in.AmountCents <= 0 {
		return domain.Payment{}, fmt.Errorf("%w: valor do pagamento deve ser maior que zero", ErrValidation)
	}
	paidAt := strings.TrimSpace(in.PaidAt)
	if paidAt == "" {
		paidAt = time.Now().UTC().Format("2006-01-02")
	}
	note := strings.TrimSpace(in.Note)

	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return domain.Payment{}, fmt.Errorf("iniciar transacao criar pagamento: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := getPersonRow(ctx, tx, in.PersonID); err != nil {
		return domain.Payment{}, err
	}

	res, err := tx.ExecContext(
		ctx,
		`INSERT INTO payments (person_id, reference_year, reference_month, amount_cents, paid_at, note) VALUES (?, ?, ?, ?, ?, ?)`,
		in.PersonID, in.ReferenceYear, in.ReferenceMonth, in.AmountCents, paidAt, note,
	)
	if err != nil {
		return domain.Payment{}, fmt.Errorf("inserir pagamento: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Payment{}, fmt.Errorf("obter id pagamento: %w", err)
	}
	if err := touchMutationExec(ctx, tx); err != nil {
		return domain.Payment{}, fmt.Errorf("atualizar mutacao: %w", err)
	}

	var p domain.Payment
	err = tx.QueryRowContext(
		ctx,
		`SELECT id, person_id, reference_year, reference_month, amount_cents, paid_at, note, created_at FROM payments WHERE id = ?`,
		id,
	).Scan(&p.ID, &p.PersonID, &p.ReferenceYear, &p.ReferenceMonth, &p.AmountCents, &p.PaidAt, &p.Note, &p.CreatedAt)
	if err != nil {
		return domain.Payment{}, fmt.Errorf("carregar pagamento criado: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Payment{}, fmt.Errorf("confirmar criacao pagamento: %w", err)
	}
	return p, nil
}

func (s *Store) DeletePayment(ctx context.Context, id int64) error {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("iniciar transacao excluir pagamento: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `DELETE FROM payments WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("excluir pagamento: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("verificar linhas excluidas pagamento: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: pagamento %d", ErrNotFound, id)
	}
	if err := touchMutationExec(ctx, tx); err != nil {
		return fmt.Errorf("atualizar mutacao: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("confirmar exclusao pagamento: %w", err)
	}
	return nil
}

type yearMonthKey struct {
	year  int
	month int
}

func (s *Store) GetPersonLedger(ctx context.Context, personID int64) (domain.PersonLedger, error) {
	conn := s.DB()
	person, err := getPersonRow(ctx, conn, personID)
	if err != nil {
		return domain.PersonLedger{}, err
	}

	monthMap := make(map[yearMonthKey]*domain.MonthLedgerRow)
	ensureMonth := func(year, month int) *domain.MonthLedgerRow {
		k := yearMonthKey{year: year, month: month}
		if row, ok := monthMap[k]; ok {
			return row
		}
		row := &domain.MonthLedgerRow{
			Year:     year,
			Month:    month,
			Payments: make([]domain.Payment, 0),
		}
		monthMap[k] = row
		return row
	}

	spentRows, err := conn.QueryContext(ctx, `
		SELECT s.reference_year, s.reference_month, COALESCE(SUM(ts.amount_cents), 0)
		FROM transaction_splits ts
		JOIN transactions t ON t.id = ts.transaction_id
		JOIN statements s ON s.id = t.statement_id
		WHERE ts.person_id = ? AND t.is_ignored = 0
		GROUP BY s.reference_year, s.reference_month
	`, personID)
	if err != nil {
		return domain.PersonLedger{}, fmt.Errorf("consultar gastos mensais pessoa: %w", err)
	}
	for spentRows.Next() {
		var year, month int
		var spent int64
		if err := spentRows.Scan(&year, &month, &spent); err != nil {
			_ = spentRows.Close()
			return domain.PersonLedger{}, fmt.Errorf("ler gasto mensal: %w", err)
		}
		row := ensureMonth(year, month)
		row.SpentCents = spent
	}
	if err := spentRows.Err(); err != nil {
		_ = spentRows.Close()
		return domain.PersonLedger{}, fmt.Errorf("iterar gastos mensais: %w", err)
	}
	_ = spentRows.Close()

	payRows, err := conn.QueryContext(ctx, `
		SELECT id, person_id, reference_year, reference_month, amount_cents, paid_at, note, created_at
		FROM payments
		WHERE person_id = ?
		ORDER BY reference_year DESC, reference_month DESC, paid_at DESC, id DESC
	`, personID)
	if err != nil {
		return domain.PersonLedger{}, fmt.Errorf("consultar pagamentos pessoa: %w", err)
	}
	for payRows.Next() {
		var pay domain.Payment
		if err := payRows.Scan(
			&pay.ID,
			&pay.PersonID,
			&pay.ReferenceYear,
			&pay.ReferenceMonth,
			&pay.AmountCents,
			&pay.PaidAt,
			&pay.Note,
			&pay.CreatedAt,
		); err != nil {
			_ = payRows.Close()
			return domain.PersonLedger{}, fmt.Errorf("ler pagamento pessoa: %w", err)
		}
		row := ensureMonth(pay.ReferenceYear, pay.ReferenceMonth)
		row.PaidCents += pay.AmountCents
		row.Payments = append(row.Payments, pay)
	}
	if err := payRows.Err(); err != nil {
		_ = payRows.Close()
		return domain.PersonLedger{}, fmt.Errorf("iterar pagamentos pessoa: %w", err)
	}
	_ = payRows.Close()

	months := make([]domain.MonthLedgerRow, 0, len(monthMap))
	var totalSpent, totalPaid int64
	for _, row := range monthMap {
		row.MonthBalanceCents = row.SpentCents - row.PaidCents
		row.Status = computeMonthStatus(row.SpentCents, row.PaidCents)
		totalSpent += row.SpentCents
		totalPaid += row.PaidCents
		months = append(months, *row)
	}

	sort.Slice(months, func(i, j int) bool {
		if months[i].Year != months[j].Year {
			return months[i].Year > months[j].Year
		}
		return months[i].Month > months[j].Month
	})

	return domain.PersonLedger{
		Person:          person,
		TotalSpentCents: totalSpent,
		TotalPaidCents:  totalPaid,
		BalanceCents:    totalSpent - totalPaid,
		Months:          months,
	}, nil
}

func computeMonthStatus(spentCents, paidCents int64) string {
	switch {
	case spentCents <= 0:
		return "sem_gastos"
	case paidCents == 0:
		return "pendente"
	case paidCents < spentCents:
		return "pago_parcial"
	default:
		return "pago"
	}
}
