package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/FelipeSrutkoske/Skatt/internal/db"
	"github.com/FelipeSrutkoske/Skatt/internal/domain"
	"github.com/FelipeSrutkoske/Skatt/internal/store"
)

func openTestStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "finance.db")
	conn, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if current := conn; current != nil {
			_ = current.Close()
		}
	})
	st := store.New(conn)
	t.Cleanup(func() {
		if current := st.DB(); current != nil {
			_ = current.Close()
		}
	})
	return st, dbPath
}

func findPersonByName(t *testing.T, st *store.Store, name string) domain.Person {
	t.Helper()
	people, err := st.ListPeople(context.Background(), true)
	if err != nil {
		t.Fatalf("list people: %v", err)
	}
	for _, p := range people {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("person %q not found", name)
	return domain.Person{}
}

func TestCreateStatementPreservesDuplicateSameDayTransactions(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()

	declared := int64(3980)
	stmt, oldPath, err := st.CreateStatement(ctx, store.CreateStatementInput{
		Bank:               "Nubank",
		CardLabel:          "Principal",
		ReferenceYear:      2025,
		ReferenceMonth:     4,
		OriginalFilename:   "nubank-2025-04.csv",
		StoredPath:         "uploads/2025/04/1_nubank.csv",
		FileSHA256:         "abc123",
		FileFormat:         "csv",
		ParserUsed:         "nubank_csv",
		DeclaredTotalCents: &declared,
		Transactions: []domain.PreviewTransaction{
			{
				LineIndex:          0,
				PurchaseDate:       "2025-04-10",
				Description:        "UBER *TRIP",
				NormalizedMerchant: "UBER TRIP",
				AmountCents:        1990,
			},
			{
				LineIndex:          1,
				PurchaseDate:       "2025-04-10",
				Description:        "UBER *TRIP",
				NormalizedMerchant: "UBER TRIP",
				AmountCents:        1990,
			},
			{
				LineIndex:          2,
				PurchaseDate:       "2025-04-11",
				Description:        "Pagamento recebido",
				NormalizedMerchant: "PAGAMENTO RECEBIDO",
				AmountCents:        -3980,
				IsIgnored:          true,
			},
		},
	})
	if err != nil {
		t.Fatalf("create statement: %v", err)
	}
	if oldPath != "" {
		t.Fatalf("expected empty oldPath, got %q", oldPath)
	}
	if stmt.Bank != "nubank" {
		t.Fatalf("expected normalized bank nubank, got %q", stmt.Bank)
	}
	if stmt.TotalCents != 3980 {
		t.Fatalf("expected total 3980, got %d", stmt.TotalCents)
	}
	if stmt.TransactionCount != 2 || stmt.UnassignedCount != 2 || stmt.ClassifiedCount != 0 {
		t.Fatalf("unexpected counts: total=%d classified=%d unassigned=%d", stmt.TransactionCount, stmt.ClassifiedCount, stmt.UnassignedCount)
	}

	detail, err := st.GetStatementDetail(ctx, stmt.ID)
	if err != nil {
		t.Fatalf("get statement detail: %v", err)
	}
	if len(detail.Transactions) != 3 {
		t.Fatalf("expected 3 transactions including ignored, got %d", len(detail.Transactions))
	}
	if detail.Transactions[0].LineIndex != 0 || detail.Transactions[1].LineIndex != 1 {
		t.Fatalf("expected line_index 0 and 1, got %d and %d", detail.Transactions[0].LineIndex, detail.Transactions[1].LineIndex)
	}
	if detail.UnassignedTotalCents != 3980 {
		t.Fatalf("expected unassigned total 3980, got %d", detail.UnassignedTotalCents)
	}

	dup, err := st.FindDuplicates(ctx, "abc123", "nubank", 2025, 4)
	if err != nil {
		t.Fatalf("find duplicates: %v", err)
	}
	if dup == nil || dup.ExactFileMatch == nil || dup.ExactFileMatch.ID != stmt.ID {
		t.Fatalf("expected exact file match for statement %d", stmt.ID)
	}
	if len(dup.PeriodBankMatches) != 1 {
		t.Fatalf("expected 1 period bank match, got %d", len(dup.PeriodBankMatches))
	}

	dupNoSHA, err := st.FindDuplicates(ctx, "", "inter", 2025, 4)
	if err != nil {
		t.Fatalf("find duplicates empty sha: %v", err)
	}
	if dupNoSHA != nil {
		t.Fatalf("expected nil duplicate warning for empty sha and unmatched bank/period")
	}
}

func TestSetTransactionSplitsValidationAndLearning(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()

	eu := findPersonByName(t, st, "Eu")
	regina, err := st.CreatePerson(ctx, "Regina", "regina@example.com", "#334155")
	if err != nil {
		t.Fatalf("create person: %v", err)
	}

	stmt, _, err := st.CreateStatement(ctx, store.CreateStatementInput{
		Bank:           "nubank",
		ReferenceYear:  2025,
		ReferenceMonth: 4,
		FileFormat:     "csv",
		ParserUsed:     "nubank_csv",
		Transactions: []domain.PreviewTransaction{
			{
				LineIndex:          0,
				PurchaseDate:       "2025-04-05",
				Description:        "AMAZON MARKETPLACE",
				NormalizedMerchant: "AMAZON MARKETPLACE",
				AmountCents:        12000,
			},
			{
				LineIndex:          1,
				PurchaseDate:       "2025-04-06",
				Description:        "ESTORNO LOJA X",
				NormalizedMerchant: "ESTORNO LOJA X",
				AmountCents:        -4500,
			},
			{
				LineIndex:          2,
				PurchaseDate:       "2025-04-07",
				Description:        "FARMACIA CENTRAL",
				NormalizedMerchant: "FARMACIA CENTRAL",
				AmountCents:        3500,
			},
		},
	})
	if err != nil {
		t.Fatalf("create statement: %v", err)
	}

	detail, err := st.GetStatementDetail(ctx, stmt.ID)
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	amazonID := detail.Transactions[0].ID
	refundID := detail.Transactions[1].ID
	pharmacyID := detail.Transactions[2].ID

	if _, err := st.SetTransactionSplits(ctx, amazonID, []domain.SplitInput{
		{PersonID: eu.ID, AmountCents: 5000},
		{PersonID: regina.ID, AmountCents: 6999},
	}); !errors.Is(err, store.ErrValidation) {
		t.Fatalf("expected validation error for wrong sum, got %v", err)
	}

	if _, err := st.SetTransactionSplits(ctx, amazonID, []domain.SplitInput{
		{PersonID: eu.ID, AmountCents: 6000},
		{PersonID: eu.ID, AmountCents: 6000},
	}); !errors.Is(err, store.ErrValidation) {
		t.Fatalf("expected validation error for duplicate person, got %v", err)
	}

	txUpdated, err := st.SetTransactionSplits(ctx, amazonID, []domain.SplitInput{
		{PersonID: eu.ID, AmountCents: 5000},
		{PersonID: regina.ID, AmountCents: 7000},
	})
	if err != nil {
		t.Fatalf("valid split failed: %v", err)
	}
	if len(txUpdated.Splits) != 2 {
		t.Fatalf("expected 2 splits, got %d", len(txUpdated.Splits))
	}

	suggestedAmazon, err := st.LookupSuggestedPerson(ctx, "AMAZON MARKETPLACE")
	if err != nil {
		t.Fatalf("lookup suggested amazon: %v", err)
	}
	if suggestedAmazon != nil {
		t.Fatalf("expected no learned rule from multi-person split, got %d", *suggestedAmazon)
	}

	refundUpdated, err := st.SetTransactionSplits(ctx, refundID, []domain.SplitInput{
		{PersonID: regina.ID, AmountCents: -4500},
	})
	if err != nil {
		t.Fatalf("negative refund split failed: %v", err)
	}
	if len(refundUpdated.Splits) != 1 || refundUpdated.Splits[0].AmountCents != -4500 {
		t.Fatalf("unexpected refund splits: %+v", refundUpdated.Splits)
	}

	if _, err := st.SetTransactionSplits(ctx, pharmacyID, []domain.SplitInput{
		{PersonID: regina.ID, AmountCents: 3500},
	}); err != nil {
		t.Fatalf("single split failed: %v", err)
	}

	suggestedPharmacy, err := st.LookupSuggestedPerson(ctx, "FARMACIA CENTRAL")
	if err != nil {
		t.Fatalf("lookup suggested pharmacy: %v", err)
	}
	if suggestedPharmacy == nil || *suggestedPharmacy != regina.ID {
		t.Fatalf("expected learned rule for regina (%d), got %v", regina.ID, suggestedPharmacy)
	}

	cleared, err := st.SetTransactionSplits(ctx, amazonID, []domain.SplitInput{})
	if err != nil {
		t.Fatalf("clear splits failed: %v", err)
	}
	if len(cleared.Splits) != 0 {
		t.Fatalf("expected 0 splits after clear, got %d", len(cleared.Splits))
	}

	stmt2, _, err := st.CreateStatement(ctx, store.CreateStatementInput{
		Bank:           "inter",
		ReferenceYear:  2025,
		ReferenceMonth: 5,
		FileFormat:     "csv",
		ParserUsed:     "inter_csv",
		Transactions: []domain.PreviewTransaction{
			{
				LineIndex:          0,
				PurchaseDate:       "2025-05-02",
				Description:        "FARMACIA CENTRAL",
				NormalizedMerchant: "FARMACIA CENTRAL",
				AmountCents:        4200,
			},
			{
				LineIndex:          1,
				PurchaseDate:       "2025-05-03",
				Description:        "PADARIA SOL",
				NormalizedMerchant: "PADARIA SOL",
				AmountCents:        1500,
			},
		},
	})
	if err != nil {
		t.Fatalf("create stmt2: %v", err)
	}

	applied, err := st.ApplySuggestions(ctx, stmt2.ID)
	if err != nil {
		t.Fatalf("apply suggestions: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected 1 suggestion applied, got %d", applied)
	}

	assigned, err := st.AssignRemaining(ctx, stmt2.ID, eu.ID)
	if err != nil {
		t.Fatalf("assign remaining: %v", err)
	}
	if assigned != 1 {
		t.Fatalf("expected 1 remaining assigned, got %d", assigned)
	}

	detail2, err := st.GetStatementDetail(ctx, stmt2.ID)
	if err != nil {
		t.Fatalf("get detail2: %v", err)
	}
	if detail2.Statement.UnassignedCount != 0 || detail2.Statement.ClassifiedCount != 2 {
		t.Fatalf("expected all classified, got %+v", detail2.Statement)
	}
}

func TestReplaceStatementCleanReplacement(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()

	eu := findPersonByName(t, st, "Eu")
	stmt1, _, err := st.CreateStatement(ctx, store.CreateStatementInput{
		Bank:           "nubank",
		ReferenceYear:  2025,
		ReferenceMonth: 4,
		StoredPath:     "uploads/2025/04/old.csv",
		FileSHA256:     "sha-old",
		FileFormat:     "csv",
		ParserUsed:     "nubank_csv",
		Transactions: []domain.PreviewTransaction{
			{
				LineIndex:          0,
				PurchaseDate:       "2025-04-01",
				Description:        "COMPRA ANTIGA",
				NormalizedMerchant: "COMPRA ANTIGA",
				AmountCents:        5000,
				Splits:             []domain.SplitInput{{PersonID: eu.ID, AmountCents: 5000}},
			},
		},
	})
	if err != nil {
		t.Fatalf("create stmt1: %v", err)
	}

	stmt2, oldPath, err := st.CreateStatement(ctx, store.CreateStatementInput{
		Bank:               "nubank",
		ReferenceYear:      2025,
		ReferenceMonth:     4,
		StoredPath:         "uploads/2025/04/new.csv",
		FileSHA256:         "sha-new",
		FileFormat:         "csv",
		ParserUsed:         "nubank_csv",
		ReplaceStatementID: &stmt1.ID,
		Transactions: []domain.PreviewTransaction{
			{
				LineIndex:          0,
				PurchaseDate:       "2025-04-02",
				Description:        "COMPRA NOVA",
				NormalizedMerchant: "COMPRA NOVA",
				AmountCents:        8000,
			},
		},
	})
	if err != nil {
		t.Fatalf("replace statement: %v", err)
	}
	if oldPath != "uploads/2025/04/old.csv" {
		t.Fatalf("expected old path uploads/2025/04/old.csv, got %q", oldPath)
	}
	if stmt2.ID == stmt1.ID {
		t.Fatalf("expected new statement id")
	}
	if _, err := st.GetStatement(ctx, stmt1.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected old statement to be deleted, got %v", err)
	}

	dash, err := st.GetDashboard(ctx, 2025, 4, "nubank")
	if err != nil {
		t.Fatalf("get dashboard: %v", err)
	}
	if dash.TotalCents != 8000 || len(dash.Statements) != 1 || dash.SelfTotalCents != 0 {
		t.Fatalf("unexpected dashboard after replacement: %+v", dash)
	}

	deletedPath, err := st.DeleteStatement(ctx, stmt2.ID)
	if err != nil {
		t.Fatalf("delete statement: %v", err)
	}
	if deletedPath != "uploads/2025/04/new.csv" {
		t.Fatalf("expected deleted path uploads/2025/04/new.csv, got %q", deletedPath)
	}
}

func TestPersonLedgerStatusAndAccumulatedBalance(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()

	regina, err := st.CreatePerson(ctx, "Regina", "regina@example.com", "#334155")
	if err != nil {
		t.Fatalf("create regina: %v", err)
	}

	_, _, err = st.CreateStatement(ctx, store.CreateStatementInput{
		Bank:           "nubank",
		ReferenceYear:  2025,
		ReferenceMonth: 3,
		FileFormat:     "manual",
		ParserUsed:     "manual",
		Transactions: []domain.PreviewTransaction{
			{
				LineIndex:          0,
				PurchaseDate:       "2025-03-10",
				Description:        "MERCADO MARCO",
				NormalizedMerchant: "MERCADO MARCO",
				AmountCents:        10000,
				Splits:             []domain.SplitInput{{PersonID: regina.ID, AmountCents: 10000}},
			},
		},
	})
	if err != nil {
		t.Fatalf("create march stmt: %v", err)
	}

	_, _, err = st.CreateStatement(ctx, store.CreateStatementInput{
		Bank:           "nubank",
		ReferenceYear:  2025,
		ReferenceMonth: 4,
		FileFormat:     "manual",
		ParserUsed:     "manual",
		Transactions: []domain.PreviewTransaction{
			{
				LineIndex:          0,
				PurchaseDate:       "2025-04-10",
				Description:        "MERCADO ABRIL",
				NormalizedMerchant: "MERCADO ABRIL",
				AmountCents:        20000,
				Splits:             []domain.SplitInput{{PersonID: regina.ID, AmountCents: 20000}},
			},
		},
	})
	if err != nil {
		t.Fatalf("create april stmt: %v", err)
	}

	_, _, err = st.CreateStatement(ctx, store.CreateStatementInput{
		Bank:           "nubank",
		ReferenceYear:  2025,
		ReferenceMonth: 5,
		FileFormat:     "manual",
		ParserUsed:     "manual",
		Transactions: []domain.PreviewTransaction{
			{
				LineIndex:          0,
				PurchaseDate:       "2025-05-10",
				Description:        "MERCADO MAIO",
				NormalizedMerchant: "MERCADO MAIO",
				AmountCents:        15000,
				Splits:             []domain.SplitInput{{PersonID: regina.ID, AmountCents: 15000}},
			},
		},
	})
	if err != nil {
		t.Fatalf("create may stmt: %v", err)
	}

	if _, err := st.CreatePayment(ctx, domain.PaymentInput{
		PersonID:       regina.ID,
		ReferenceYear:  2025,
		ReferenceMonth: 3,
		AmountCents:    10000,
		PaidAt:         "2025-04-01",
		Note:           "PIX marco",
	}); err != nil {
		t.Fatalf("create march payment: %v", err)
	}

	payApril, err := st.CreatePayment(ctx, domain.PaymentInput{
		PersonID:       regina.ID,
		ReferenceYear:  2025,
		ReferenceMonth: 4,
		AmountCents:    8000,
		PaidAt:         "2025-05-01",
		Note:           "PIX parcial abril",
	})
	if err != nil {
		t.Fatalf("create april payment: %v", err)
	}

	if _, err := st.CreatePayment(ctx, domain.PaymentInput{
		PersonID:       regina.ID,
		ReferenceYear:  2025,
		ReferenceMonth: 6,
		AmountCents:    2000,
		PaidAt:         "2025-06-05",
		Note:           "Adiantamento",
	}); err != nil {
		t.Fatalf("create june payment: %v", err)
	}

	ledger, err := st.GetPersonLedger(ctx, regina.ID)
	if err != nil {
		t.Fatalf("get ledger: %v", err)
	}
	if ledger.TotalSpentCents != 45000 || ledger.TotalPaidCents != 20000 || ledger.BalanceCents != 25000 {
		t.Fatalf("unexpected ledger totals: spent=%d paid=%d balance=%d", ledger.TotalSpentCents, ledger.TotalPaidCents, ledger.BalanceCents)
	}

	statuses := make(map[int]string)
	for _, m := range ledger.Months {
		statuses[m.Month] = m.Status
	}
	if statuses[3] != "pago" {
		t.Fatalf("expected march status pago, got %q", statuses[3])
	}
	if statuses[4] != "pago_parcial" {
		t.Fatalf("expected april status pago_parcial, got %q", statuses[4])
	}
	if statuses[5] != "pendente" {
		t.Fatalf("expected may status pendente, got %q", statuses[5])
	}
	if statuses[6] != "sem_gastos" {
		t.Fatalf("expected june status sem_gastos, got %q", statuses[6])
	}

	if err := st.DeletePayment(ctx, payApril.ID); err != nil {
		t.Fatalf("delete april payment: %v", err)
	}
	ledgerAfter, err := st.GetPersonLedger(ctx, regina.ID)
	if err != nil {
		t.Fatalf("get ledger after delete: %v", err)
	}
	if ledgerAfter.BalanceCents != 33000 {
		t.Fatalf("expected balance 33000 after payment delete, got %d", ledgerAfter.BalanceCents)
	}
}

func TestComputeReceiptDataSingleBankVsConsolidated(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()

	eu := findPersonByName(t, st, "Eu")
	regina, err := st.CreatePerson(ctx, "Regina", "regina@example.com", "#334155")
	if err != nil {
		t.Fatalf("create regina: %v", err)
	}

	_, _, err = st.CreateStatement(ctx, store.CreateStatementInput{
		Bank:           "nubank",
		ReferenceYear:  2025,
		ReferenceMonth: 3,
		FileFormat:     "manual",
		ParserUsed:     "manual",
		Transactions: []domain.PreviewTransaction{
			{
				LineIndex:          0,
				PurchaseDate:       "2025-03-15",
				Description:        "COMPRA ANTERIOR",
				NormalizedMerchant: "COMPRA ANTERIOR",
				AmountCents:        10000,
				Splits:             []domain.SplitInput{{PersonID: regina.ID, AmountCents: 10000}},
			},
		},
	})
	if err != nil {
		t.Fatalf("create march stmt: %v", err)
	}

	if _, err := st.CreatePayment(ctx, domain.PaymentInput{
		PersonID:       regina.ID,
		ReferenceYear:  2025,
		ReferenceMonth: 3,
		AmountCents:    4000,
		PaidAt:         "2025-03-28",
	}); err != nil {
		t.Fatalf("create march payment: %v", err)
	}

	_, _, err = st.CreateStatement(ctx, store.CreateStatementInput{
		Bank:           "nubank",
		ReferenceYear:  2025,
		ReferenceMonth: 4,
		FileFormat:     "manual",
		ParserUsed:     "manual",
		Transactions: []domain.PreviewTransaction{
			{
				LineIndex:          0,
				PurchaseDate:       "2025-04-05",
				Description:        "AMAZON",
				NormalizedMerchant: "AMAZON",
				AmountCents:        12000,
				Splits: []domain.SplitInput{
					{PersonID: eu.ID, AmountCents: 5000},
					{PersonID: regina.ID, AmountCents: 7000},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("create april nubank stmt: %v", err)
	}

	_, _, err = st.CreateStatement(ctx, store.CreateStatementInput{
		Bank:           "inter",
		ReferenceYear:  2025,
		ReferenceMonth: 4,
		FileFormat:     "manual",
		ParserUsed:     "manual",
		Transactions: []domain.PreviewTransaction{
			{
				LineIndex:          0,
				PurchaseDate:       "2025-04-08",
				Description:        "MERCADO INTER",
				NormalizedMerchant: "MERCADO INTER",
				AmountCents:        5000,
				Splits:             []domain.SplitInput{{PersonID: regina.ID, AmountCents: 5000}},
			},
		},
	})
	if err != nil {
		t.Fatalf("create april inter stmt: %v", err)
	}

	if _, err := st.CreatePayment(ctx, domain.PaymentInput{
		PersonID:       regina.ID,
		ReferenceYear:  2025,
		ReferenceMonth: 4,
		AmountCents:    3000,
		PaidAt:         "2025-04-20",
	}); err != nil {
		t.Fatalf("create april payment: %v", err)
	}

	singleBank, err := st.ComputeReceiptData(ctx, store.ReceiptQuery{
		PersonID:            regina.ID,
		ReferenceYear:       2025,
		ReferenceMonth:      4,
		BankFilter:          "nubank",
		IncludePriorBalance: true,
	})
	if err != nil {
		t.Fatalf("single bank receipt: %v", err)
	}
	if singleBank.SubtotalCents != 7000 || singleBank.PaidCents != 0 || singleBank.PriorBalanceCents != 0 || singleBank.TotalCents != 7000 {
		t.Fatalf("unexpected single bank receipt totals: %+v", singleBank)
	}
	if singleBank.IncludePriorBalance {
		t.Fatalf("expected IncludePriorBalance=false when bank filter is set")
	}
	if len(singleBank.Items) != 1 || !singleBank.Items[0].IsSplit {
		t.Fatalf("expected 1 split item for nubank, got %+v", singleBank.Items)
	}

	consolidatedNoPrior, err := st.ComputeReceiptData(ctx, store.ReceiptQuery{
		PersonID:            regina.ID,
		ReferenceYear:       2025,
		ReferenceMonth:      4,
		BankFilter:          "",
		IncludePriorBalance: false,
	})
	if err != nil {
		t.Fatalf("consolidated no prior: %v", err)
	}
	if consolidatedNoPrior.SubtotalCents != 12000 || consolidatedNoPrior.PaidCents != 3000 || consolidatedNoPrior.PriorBalanceCents != 0 || consolidatedNoPrior.TotalCents != 9000 {
		t.Fatalf("unexpected consolidated no prior totals: %+v", consolidatedNoPrior)
	}

	consolidatedWithPrior, err := st.ComputeReceiptData(ctx, store.ReceiptQuery{
		PersonID:            regina.ID,
		ReferenceYear:       2025,
		ReferenceMonth:      4,
		BankFilter:          "",
		IncludePriorBalance: true,
	})
	if err != nil {
		t.Fatalf("consolidated with prior: %v", err)
	}
	if consolidatedWithPrior.SubtotalCents != 12000 || consolidatedWithPrior.PaidCents != 3000 || consolidatedWithPrior.PriorBalanceCents != 6000 || consolidatedWithPrior.TotalCents != 15000 {
		t.Fatalf("unexpected consolidated with prior totals: %+v", consolidatedWithPrior)
	}

	saved, err := st.SaveReceipt(ctx, domain.Receipt{
		PersonID:            regina.ID,
		ReferenceYear:       2025,
		ReferenceMonth:      4,
		BankFilter:          "",
		IncludePriorBalance: true,
		RecipientEmail:      "regina@example.com",
		Subject:             "Resumo Abril 2025",
		HTMLBody:            "<html>ok</html>",
		SubtotalCents:       consolidatedWithPrior.SubtotalCents,
		PaidCents:           consolidatedWithPrior.PaidCents,
		PriorBalanceCents:   consolidatedWithPrior.PriorBalanceCents,
		TotalCents:          consolidatedWithPrior.TotalCents,
		DeliveryStatus:      "generated",
	})
	if err != nil {
		t.Fatalf("save receipt: %v", err)
	}
	if saved.ID == 0 || saved.PersonName != "Regina" {
		t.Fatalf("unexpected saved receipt: %+v", saved)
	}

	fetched, err := st.GetReceipt(ctx, saved.ID)
	if err != nil {
		t.Fatalf("get receipt: %v", err)
	}
	if fetched.TotalCents != 15000 {
		t.Fatalf("expected fetched total 15000, got %d", fetched.TotalCents)
	}

	list, err := st.ListReceipts(ctx)
	if err != nil {
		t.Fatalf("list receipts: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(list))
	}
}

func TestDeletePersonGuardsSelfAndReferenced(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()

	eu := findPersonByName(t, st, "Eu")
	if err := st.DeletePerson(ctx, eu.ID); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("expected ErrForbidden when deleting self, got %v", err)
	}

	withSplit, err := st.CreatePerson(ctx, "Joao", "joao@example.com", "#475569")
	if err != nil {
		t.Fatalf("create joao: %v", err)
	}
	_, _, err = st.CreateStatement(ctx, store.CreateStatementInput{
		Bank:           "nubank",
		ReferenceYear:  2025,
		ReferenceMonth: 4,
		FileFormat:     "manual",
		ParserUsed:     "manual",
		Transactions: []domain.PreviewTransaction{
			{
				LineIndex:          0,
				PurchaseDate:       "2025-04-01",
				Description:        "TESTE",
				NormalizedMerchant: "TESTE",
				AmountCents:        1000,
				Splits:             []domain.SplitInput{{PersonID: withSplit.ID, AmountCents: 1000}},
			},
		},
	})
	if err != nil {
		t.Fatalf("create stmt: %v", err)
	}
	if err := st.DeletePerson(ctx, withSplit.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict when deleting person with splits, got %v", err)
	}

	withPayment, err := st.CreatePerson(ctx, "Maria", "maria@example.com", "#475569")
	if err != nil {
		t.Fatalf("create maria: %v", err)
	}
	if _, err := st.CreatePayment(ctx, domain.PaymentInput{
		PersonID:       withPayment.ID,
		ReferenceYear:  2025,
		ReferenceMonth: 4,
		AmountCents:    500,
		PaidAt:         "2025-04-10",
	}); err != nil {
		t.Fatalf("create payment: %v", err)
	}
	if err := st.DeletePerson(ctx, withPayment.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict when deleting person with payment, got %v", err)
	}

	withReceipt, err := st.CreatePerson(ctx, "Carlos", "carlos@example.com", "#475569")
	if err != nil {
		t.Fatalf("create carlos: %v", err)
	}
	if _, err := st.SaveReceipt(ctx, domain.Receipt{
		PersonID:       withReceipt.ID,
		ReferenceYear:  2025,
		ReferenceMonth: 4,
		Subject:        "Recibo",
		HTMLBody:       "<p>ok</p>",
		SubtotalCents:  0,
		TotalCents:     0,
		DeliveryStatus: "generated",
	}); err != nil {
		t.Fatalf("save receipt: %v", err)
	}
	if err := st.DeletePerson(ctx, withReceipt.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict when deleting person with receipt, got %v", err)
	}

	freePerson, err := st.CreatePerson(ctx, "Temporario", "", "#475569")
	if err != nil {
		t.Fatalf("create temp person: %v", err)
	}
	if err := st.DeletePerson(ctx, freePerson.ID); err != nil {
		t.Fatalf("expected deleting unreferenced person to succeed, got %v", err)
	}
}

func TestSettingsBackupReminderAndReplaceDB(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()

	if err := st.SetSettings(ctx, map[string]string{
		"smtp_host":      "smtp.gmail.com",
		"smtp_port":      "587",
		"smtp_user":      "user@gmail.com",
		"smtp_pass":      "secret-pass",
		"smtp_from_name": "Finance",
		"ai_api_key":     "gemini-secret",
	}); err != nil {
		t.Fatalf("set settings: %v", err)
	}

	view, err := st.GetSettingsView(ctx)
	if err != nil {
		t.Fatalf("get settings view: %v", err)
	}
	if !view.HasSMTPPass || !view.HasAIAPIKey || view.SMTPPort != 587 || view.BackupReminderDays != 14 {
		t.Fatalf("unexpected settings view: %+v", view)
	}

	if err := st.TouchMutation(ctx); err != nil {
		t.Fatalf("touch mutation: %v", err)
	}
	dash1, err := st.GetDashboard(ctx, 0, 0, "")
	if err != nil {
		t.Fatalf("dashboard after mutation: %v", err)
	}
	if !dash1.BackupReminder.NeedsBackup {
		t.Fatalf("expected NeedsBackup=true after mutation with no backup")
	}

	if err := st.TouchBackup(ctx); err != nil {
		t.Fatalf("touch backup: %v", err)
	}
	dash2, err := st.GetDashboard(ctx, 0, 0, "")
	if err != nil {
		t.Fatalf("dashboard after backup: %v", err)
	}
	if dash2.BackupReminder.NeedsBackup {
		t.Fatalf("expected NeedsBackup=false immediately after backup")
	}

	newDBPath := filepath.Join(t.TempDir(), "replaced.db")
	newConn, err := db.Open(newDBPath)
	if err != nil {
		t.Fatalf("open replacement db: %v", err)
	}
	st.ReplaceDB(newConn)
	if st.DB() != newConn {
		t.Fatalf("expected DB() to return replacement connection")
	}

	viewAfter, err := st.GetSettingsView(ctx)
	if err != nil {
		t.Fatalf("get settings view after replace: %v", err)
	}
	if viewAfter.HasSMTPPass || viewAfter.HasAIAPIKey {
		t.Fatalf("expected fresh settings in replacement db, got %+v", viewAfter)
	}
}

func TestUpdateTransactionAndDashboardFilters(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()

	eu := findPersonByName(t, st, "Eu")
	stmt, _, err := st.CreateStatement(ctx, store.CreateStatementInput{
		Bank:           "itau",
		ReferenceYear:  2026,
		ReferenceMonth: 1,
		FileFormat:     "manual",
		ParserUsed:     "manual",
		Transactions: []domain.PreviewTransaction{
			{
				LineIndex:          0,
				PurchaseDate:       "2026-01-12",
				Description:        "SUPERMERCADO",
				NormalizedMerchant: "SUPERMERCADO",
				AmountCents:        10000,
				Splits:             []domain.SplitInput{{PersonID: eu.ID, AmountCents: 10000}},
			},
		},
	})
	if err != nil {
		t.Fatalf("create stmt: %v", err)
	}

	detail, err := st.GetStatementDetail(ctx, stmt.ID)
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	txID := detail.Transactions[0].ID

	newAmount := int64(12500)
	updated, err := st.UpdateTransaction(ctx, txID, store.UpdateTransactionInput{
		AmountCents: &newAmount,
	})
	if err != nil {
		t.Fatalf("update transaction amount: %v", err)
	}
	if updated.AmountCents != 12500 || len(updated.Splits) != 1 || updated.Splits[0].AmountCents != 12500 {
		t.Fatalf("expected single split adjusted to 12500, got %+v", updated)
	}

	ignored := true
	updatedIgnored, err := st.UpdateTransaction(ctx, txID, store.UpdateTransactionInput{
		IsIgnored: &ignored,
	})
	if err != nil {
		t.Fatalf("update transaction ignore: %v", err)
	}
	if !updatedIgnored.IsIgnored {
		t.Fatalf("expected transaction ignored")
	}

	dash, err := st.GetDashboard(ctx, 2026, 0, "")
	if err != nil {
		t.Fatalf("get dashboard: %v", err)
	}
	if dash.TotalCents != 0 || dash.SelfTotalCents != 0 || len(dash.AvailableYears) != 1 || dash.AvailableYears[0] != 2026 {
		t.Fatalf("unexpected dashboard after ignore: %+v", dash)
	}
}

func TestDashboardMonthlyBreakdown(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()

	eu := findPersonByName(t, st, "Eu")
	regina, err := st.CreatePerson(ctx, "Regina", "regina@example.com", "#1d4e89")
	if err != nil {
		t.Fatalf("create regina: %v", err)
	}

	_, _, err = st.CreateStatement(ctx, store.CreateStatementInput{
		Bank:           "nubank",
		ReferenceYear:  2025,
		ReferenceMonth: 4,
		FileFormat:     "manual",
		ParserUsed:     "manual",
		Transactions: []domain.PreviewTransaction{
			{
				LineIndex:          0,
				PurchaseDate:       "2025-04-05",
				Description:        "AMAZON",
				NormalizedMerchant: "AMAZON",
				AmountCents:        12000,
				Splits: []domain.SplitInput{
					{PersonID: eu.ID, AmountCents: 5000},
					{PersonID: regina.ID, AmountCents: 7000},
				},
			},
			{
				LineIndex:          1,
				PurchaseDate:       "2025-04-09",
				Description:        "UBER",
				NormalizedMerchant: "UBER",
				AmountCents:        2500,
			},
		},
	})
	if err != nil {
		t.Fatalf("create april nubank: %v", err)
	}

	_, _, err = st.CreateStatement(ctx, store.CreateStatementInput{
		Bank:           "inter",
		ReferenceYear:  2025,
		ReferenceMonth: 4,
		FileFormat:     "manual",
		ParserUsed:     "manual",
		Transactions: []domain.PreviewTransaction{
			{
				LineIndex:          0,
				PurchaseDate:       "2025-04-12",
				Description:        "FARMACIA",
				NormalizedMerchant: "FARMACIA",
				AmountCents:        4000,
				Splits:             []domain.SplitInput{{PersonID: regina.ID, AmountCents: 4000}},
			},
		},
	})
	if err != nil {
		t.Fatalf("create april inter: %v", err)
	}

	dashAll, err := st.GetDashboard(ctx, 2025, 0, "")
	if err != nil {
		t.Fatalf("get dashboard all banks: %v", err)
	}
	if len(dashAll.Months) != 12 {
		t.Fatalf("expected 12 months, got %d", len(dashAll.Months))
	}
	april := dashAll.Months[3]
	if april.Month != 4 || april.StatementCount != 2 || april.TotalCents != 18500 {
		t.Fatalf("unexpected april summary: %+v", april)
	}
	if april.SelfTotalCents != 5000 || april.OthersTotalCents != 11000 || april.UnassignedTotalCents != 2500 || april.UnassignedCount != 1 {
		t.Fatalf("unexpected april breakdown: %+v", april)
	}
	if len(april.TotalsByPerson) != 2 || april.TotalsByPerson[0].PersonName != "Regina" || april.TotalsByPerson[0].TotalCents != 11000 {
		t.Fatalf("unexpected april totals by person: %+v", april.TotalsByPerson)
	}
	if dashAll.Months[0].StatementCount != 0 || dashAll.Months[0].TotalCents != 0 {
		t.Fatalf("expected empty january, got %+v", dashAll.Months[0])
	}

	dashNubank, err := st.GetDashboard(ctx, 2025, 4, "nubank")
	if err != nil {
		t.Fatalf("get dashboard nubank: %v", err)
	}
	if len(dashNubank.Months) != 12 {
		t.Fatalf("expected 12 months even with month filter, got %d", len(dashNubank.Months))
	}
	aprilNubank := dashNubank.Months[3]
	if aprilNubank.StatementCount != 1 || aprilNubank.TotalCents != 14500 || aprilNubank.OthersTotalCents != 7000 {
		t.Fatalf("unexpected april nubank summary: %+v", aprilNubank)
	}

	availAll, err := st.GetReceiptAvailability(ctx, regina.ID, 2025, "")
	if err != nil {
		t.Fatalf("get receipt availability all: %v", err)
	}
	if len(availAll) != 12 || availAll[3].TotalCents != 11000 || availAll[3].ItemCount != 2 || availAll[0].ItemCount != 0 {
		t.Fatalf("unexpected receipt availability all: %+v", availAll[3])
	}

	availNubank, err := st.GetReceiptAvailability(ctx, regina.ID, 2025, "nubank")
	if err != nil {
		t.Fatalf("get receipt availability nubank: %v", err)
	}
	if availNubank[3].TotalCents != 7000 || availNubank[3].ItemCount != 1 {
		t.Fatalf("unexpected receipt availability nubank: %+v", availNubank[3])
	}
}
