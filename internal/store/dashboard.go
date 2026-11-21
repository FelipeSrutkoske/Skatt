package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
)

func (s *Store) GetDashboard(ctx context.Context, year, month int, bank string) (domain.DashboardSummary, error) {
	conn := s.DB()
	normalizedBank := ""
	if strings.TrimSpace(bank) != "" {
		normalizedBank = domain.NormalizeBank(bank)
	}

	availableYears := make([]int, 0)
	yearRows, err := conn.QueryContext(ctx, `SELECT DISTINCT reference_year FROM statements ORDER BY reference_year DESC`)
	if err != nil {
		return domain.DashboardSummary{}, fmt.Errorf("listar anos disponiveis: %w", err)
	}
	for yearRows.Next() {
		var y int
		if err := yearRows.Scan(&y); err != nil {
			_ = yearRows.Close()
			return domain.DashboardSummary{}, fmt.Errorf("ler ano disponivel: %w", err)
		}
		availableYears = append(availableYears, y)
	}
	if err := yearRows.Err(); err != nil {
		_ = yearRows.Close()
		return domain.DashboardSummary{}, fmt.Errorf("iterar anos disponiveis: %w", err)
	}
	_ = yearRows.Close()

	availableBanks := make([]string, 0)
	bankRows, err := conn.QueryContext(ctx, `SELECT DISTINCT bank FROM statements ORDER BY bank ASC`)
	if err != nil {
		return domain.DashboardSummary{}, fmt.Errorf("listar bancos disponiveis: %w", err)
	}
	for bankRows.Next() {
		var b string
		if err := bankRows.Scan(&b); err != nil {
			_ = bankRows.Close()
			return domain.DashboardSummary{}, fmt.Errorf("ler banco disponivel: %w", err)
		}
		availableBanks = append(availableBanks, b)
	}
	if err := bankRows.Err(); err != nil {
		_ = bankRows.Close()
		return domain.DashboardSummary{}, fmt.Errorf("iterar bancos disponiveis: %w", err)
	}
	_ = bankRows.Close()

	whereClauses := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if year > 0 {
		whereClauses = append(whereClauses, "s.reference_year = ?")
		args = append(args, year)
	}
	if month >= 1 && month <= 12 {
		whereClauses = append(whereClauses, "s.reference_month = ?")
		args = append(args, month)
	}
	if normalizedBank != "" {
		whereClauses = append(whereClauses, "s.bank = ?")
		args = append(args, normalizedBank)
	}

	stmtWhere := ""
	if len(whereClauses) > 0 {
		stmtWhere = " WHERE " + strings.Join(whereClauses, " AND ")
	}

	statements := make([]domain.Statement, 0)
	stmtRows, err := conn.QueryContext(
		ctx,
		statementSummarySelect+stmtWhere+` ORDER BY s.reference_year DESC, s.reference_month DESC, s.bank ASC, s.id DESC`,
		args...,
	)
	if err != nil {
		return domain.DashboardSummary{}, fmt.Errorf("listar faturas dashboard: %w", err)
	}
	var totalCents int64
	for stmtRows.Next() {
		st, err := scanStatementSummary(stmtRows)
		if err != nil {
			_ = stmtRows.Close()
			return domain.DashboardSummary{}, fmt.Errorf("ler fatura dashboard: %w", err)
		}
		totalCents += st.TotalCents
		statements = append(statements, st)
	}
	if err := stmtRows.Err(); err != nil {
		_ = stmtRows.Close()
		return domain.DashboardSummary{}, fmt.Errorf("iterar faturas dashboard: %w", err)
	}
	_ = stmtRows.Close()

	txWhere := "WHERE t.is_ignored = 0"
	if len(whereClauses) > 0 {
		txWhere += " AND " + strings.Join(whereClauses, " AND ")
	}

	totalsByPerson := make([]domain.PersonTotal, 0)
	personRows, err := conn.QueryContext(ctx, `
		SELECT p.id, p.name, p.is_self, p.color, COALESCE(SUM(ts.amount_cents), 0)
		FROM transaction_splits ts
		JOIN transactions t ON t.id = ts.transaction_id
		JOIN statements s ON s.id = t.statement_id
		JOIN people p ON p.id = ts.person_id
		`+txWhere+`
		GROUP BY p.id, p.name, p.is_self, p.color
		ORDER BY p.is_self DESC, p.name ASC
	`, args...)
	if err != nil {
		return domain.DashboardSummary{}, fmt.Errorf("calcular totais por pessoa dashboard: %w", err)
	}
	var selfTotalCents, othersTotalCents int64
	for personRows.Next() {
		var pt domain.PersonTotal
		var isSelf int
		if err := personRows.Scan(&pt.PersonID, &pt.PersonName, &isSelf, &pt.Color, &pt.TotalCents); err != nil {
			_ = personRows.Close()
			return domain.DashboardSummary{}, fmt.Errorf("ler total por pessoa dashboard: %w", err)
		}
		pt.IsSelf = intToBool(isSelf)
		if pt.IsSelf {
			selfTotalCents += pt.TotalCents
		} else {
			othersTotalCents += pt.TotalCents
		}
		totalsByPerson = append(totalsByPerson, pt)
	}
	if err := personRows.Err(); err != nil {
		_ = personRows.Close()
		return domain.DashboardSummary{}, fmt.Errorf("iterar totais por pessoa dashboard: %w", err)
	}
	_ = personRows.Close()

	var unassignedTotalCents int64
	var unassignedCount int
	err = conn.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(t.amount_cents), 0), COUNT(1)
		FROM transactions t
		JOIN statements s ON s.id = t.statement_id
		`+txWhere+`
		  AND NOT EXISTS (SELECT 1 FROM transaction_splits ts WHERE ts.transaction_id = t.id)
	`, args...).Scan(&unassignedTotalCents, &unassignedCount)
	if err != nil {
		return domain.DashboardSummary{}, fmt.Errorf("calcular nao atribuidas dashboard: %w", err)
	}

	debtByPerson := make([]domain.PersonDebtSummary, 0)
	debtRows, err := conn.QueryContext(ctx, `
		SELECT
			p.id,
			p.name,
			p.email,
			p.is_self,
			p.color,
			p.archived,
			COALESCE((
				SELECT SUM(ts.amount_cents)
				FROM transaction_splits ts
				JOIN transactions t ON t.id = ts.transaction_id
				WHERE ts.person_id = p.id AND t.is_ignored = 0
			), 0) AS total_spent_cents,
			COALESCE((
				SELECT SUM(pay.amount_cents)
				FROM payments pay
				WHERE pay.person_id = p.id
			), 0) AS total_paid_cents
		FROM people p
		ORDER BY p.is_self DESC, p.archived ASC, p.name ASC
	`)
	if err != nil {
		return domain.DashboardSummary{}, fmt.Errorf("calcular dividas acumuladas: %w", err)
	}
	for debtRows.Next() {
		var d domain.PersonDebtSummary
		var isSelf, archived int
		if err := debtRows.Scan(
			&d.PersonID,
			&d.PersonName,
			&d.Email,
			&isSelf,
			&d.Color,
			&archived,
			&d.TotalSpentCents,
			&d.TotalPaidCents,
		); err != nil {
			_ = debtRows.Close()
			return domain.DashboardSummary{}, fmt.Errorf("ler divida acumulada: %w", err)
		}
		d.IsSelf = intToBool(isSelf)
		d.Archived = intToBool(archived)
		d.BalanceCents = d.TotalSpentCents - d.TotalPaidCents
		debtByPerson = append(debtByPerson, d)
	}
	if err := debtRows.Err(); err != nil {
		_ = debtRows.Close()
		return domain.DashboardSummary{}, fmt.Errorf("iterar dividas acumuladas: %w", err)
	}
	_ = debtRows.Close()

	reminder, err := s.getBackupReminderStatus(ctx)
	if err != nil {
		return domain.DashboardSummary{}, err
	}

	months, err := s.getMonthlyBreakdown(ctx, year, normalizedBank)
	if err != nil {
		return domain.DashboardSummary{}, err
	}

	return domain.DashboardSummary{
		Year:                 year,
		Month:                month,
		Bank:                 normalizedBank,
		AvailableYears:       availableYears,
		AvailableBanks:       availableBanks,
		TotalCents:           totalCents,
		SelfTotalCents:       selfTotalCents,
		OthersTotalCents:     othersTotalCents,
		UnassignedTotalCents: unassignedTotalCents,
		UnassignedCount:      unassignedCount,
		TotalsByPerson:       totalsByPerson,
		DebtByPerson:         debtByPerson,
		Statements:           statements,
		Months:               months,
		BackupReminder:       reminder,
	}, nil
}

func (s *Store) getMonthlyBreakdown(ctx context.Context, year int, normalizedBank string) ([]domain.MonthDashboardSummary, error) {
	conn := s.DB()
	months := make([]domain.MonthDashboardSummary, 12)
	for i := 0; i < 12; i++ {
		months[i] = domain.MonthDashboardSummary{
			Month:          i + 1,
			TotalsByPerson: make([]domain.PersonTotal, 0),
			Statements:     make([]domain.Statement, 0),
		}
	}

	whereClauses := make([]string, 0, 2)
	args := make([]any, 0, 2)
	if year > 0 {
		whereClauses = append(whereClauses, "s.reference_year = ?")
		args = append(args, year)
	}
	if normalizedBank != "" {
		whereClauses = append(whereClauses, "s.bank = ?")
		args = append(args, normalizedBank)
	}

	stmtWhere := ""
	if len(whereClauses) > 0 {
		stmtWhere = " WHERE " + strings.Join(whereClauses, " AND ")
	}

	stmtRows, err := conn.QueryContext(
		ctx,
		statementSummarySelect+stmtWhere+` ORDER BY s.reference_month ASC, s.bank ASC, s.id DESC`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("listar faturas mensais dashboard: %w", err)
	}
	for stmtRows.Next() {
		st, err := scanStatementSummary(stmtRows)
		if err != nil {
			_ = stmtRows.Close()
			return nil, fmt.Errorf("ler fatura mensal dashboard: %w", err)
		}
		if st.ReferenceMonth >= 1 && st.ReferenceMonth <= 12 {
			idx := st.ReferenceMonth - 1
			months[idx].Statements = append(months[idx].Statements, st)
			months[idx].StatementCount++
			months[idx].TotalCents += st.TotalCents
			months[idx].TransactionCount += st.TransactionCount
			months[idx].ClassifiedCount += st.ClassifiedCount
			months[idx].UnassignedCount += st.UnassignedCount
		}
	}
	if err := stmtRows.Err(); err != nil {
		_ = stmtRows.Close()
		return nil, fmt.Errorf("iterar faturas mensais dashboard: %w", err)
	}
	_ = stmtRows.Close()

	txWhere := "WHERE t.is_ignored = 0"
	if len(whereClauses) > 0 {
		txWhere += " AND " + strings.Join(whereClauses, " AND ")
	}

	personMonthRows, err := conn.QueryContext(ctx, `
		SELECT
			s.reference_month,
			p.id,
			p.name,
			p.is_self,
			p.color,
			COALESCE(SUM(ts.amount_cents), 0)
		FROM transaction_splits ts
		JOIN transactions t ON t.id = ts.transaction_id
		JOIN statements s ON s.id = t.statement_id
		JOIN people p ON p.id = ts.person_id
		`+txWhere+`
		GROUP BY s.reference_month, p.id, p.name, p.is_self, p.color
		ORDER BY s.reference_month ASC, COALESCE(SUM(ts.amount_cents), 0) DESC, p.is_self DESC, p.name ASC
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("calcular splits mensais por pessoa dashboard: %w", err)
	}
	for personMonthRows.Next() {
		var m int
		var pt domain.PersonTotal
		var isSelf int
		if err := personMonthRows.Scan(&m, &pt.PersonID, &pt.PersonName, &isSelf, &pt.Color, &pt.TotalCents); err != nil {
			_ = personMonthRows.Close()
			return nil, fmt.Errorf("ler split mensal por pessoa dashboard: %w", err)
		}
		pt.IsSelf = intToBool(isSelf)
		if m >= 1 && m <= 12 {
			idx := m - 1
			months[idx].TotalsByPerson = append(months[idx].TotalsByPerson, pt)
			if pt.IsSelf {
				months[idx].SelfTotalCents += pt.TotalCents
			} else {
				months[idx].OthersTotalCents += pt.TotalCents
			}
		}
	}
	if err := personMonthRows.Err(); err != nil {
		_ = personMonthRows.Close()
		return nil, fmt.Errorf("iterar splits mensais por pessoa dashboard: %w", err)
	}
	_ = personMonthRows.Close()

	unassignedRows, err := conn.QueryContext(ctx, `
		SELECT
			s.reference_month,
			COALESCE(SUM(t.amount_cents), 0)
		FROM transactions t
		JOIN statements s ON s.id = t.statement_id
		`+txWhere+`
		  AND NOT EXISTS (SELECT 1 FROM transaction_splits ts WHERE ts.transaction_id = t.id)
		GROUP BY s.reference_month
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("calcular nao atribuidas mensais dashboard: %w", err)
	}
	for unassignedRows.Next() {
		var m int
		var unassignedCents int64
		if err := unassignedRows.Scan(&m, &unassignedCents); err != nil {
			_ = unassignedRows.Close()
			return nil, fmt.Errorf("ler nao atribuida mensal dashboard: %w", err)
		}
		if m >= 1 && m <= 12 {
			months[m-1].UnassignedTotalCents = unassignedCents
		}
	}
	if err := unassignedRows.Err(); err != nil {
		_ = unassignedRows.Close()
		return nil, fmt.Errorf("iterar nao atribuidas mensais dashboard: %w", err)
	}
	_ = unassignedRows.Close()

	return months, nil
}

func (s *Store) getBackupReminderStatus(ctx context.Context) (domain.BackupReminderStatus, error) {
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return domain.BackupReminderStatus{}, err
	}

	lastBackup := strings.TrimSpace(settings["last_backup_at"])
	lastMutation := strings.TrimSpace(settings["last_data_mutation_at"])
	reminderDays := 14
	if rawDays := strings.TrimSpace(settings["backup_reminder_days"]); rawDays != "" {
		if parsed, err := strconv.Atoi(rawDays); err == nil && parsed > 0 {
			reminderDays = parsed
		}
	}

	status := domain.BackupReminderStatus{
		LastBackupAt:   lastBackup,
		LastMutationAt: lastMutation,
	}

	backupTime, hasBackup := parseTimestamp(lastBackup)
	mutationTime, hasMutation := parseTimestamp(lastMutation)

	if hasBackup {
		days := int(time.Since(backupTime).Hours() / 24)
		if days < 0 {
			days = 0
		}
		status.DaysSinceBackup = &days
		if hasMutation && mutationTime.After(backupTime) {
			status.NeedsBackup = true
		} else if days >= reminderDays {
			status.NeedsBackup = true
		}
	} else if hasMutation {
		status.NeedsBackup = true
	}

	return status, nil
}
