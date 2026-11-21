package domain

type Person struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	IsSelf    bool   `json:"is_self"`
	Color     string `json:"color"`
	Archived  bool   `json:"archived"`
	CreatedAt string `json:"created_at"`
}

type Statement struct {
	ID                 int64  `json:"id"`
	Bank               string `json:"bank"`
	CardLabel          string `json:"card_label"`
	ReferenceYear      int    `json:"reference_year"`
	ReferenceMonth     int    `json:"reference_month"`
	OriginalFilename   string `json:"original_filename"`
	StoredPath         string `json:"stored_path"`
	FileSHA256         string `json:"file_sha256"`
	FileFormat         string `json:"file_format"`
	ParserUsed         string `json:"parser_used"`
	DeclaredTotalCents *int64 `json:"declared_total_cents"`
	ImportedAt         string `json:"imported_at"`
	TotalCents         int64  `json:"total_cents"`
	TransactionCount   int    `json:"transaction_count"`
	ClassifiedCount    int    `json:"classified_count"`
	UnassignedCount    int    `json:"unassigned_count"`
}

type TransactionSplit struct {
	ID            int64  `json:"id"`
	TransactionID int64  `json:"transaction_id"`
	PersonID      int64  `json:"person_id"`
	PersonName    string `json:"person_name"`
	AmountCents   int64  `json:"amount_cents"`
}

type SplitInput struct {
	PersonID    int64 `json:"person_id"`
	AmountCents int64 `json:"amount_cents"`
}

type Transaction struct {
	ID                 int64              `json:"id"`
	StatementID        int64              `json:"statement_id"`
	LineIndex          int                `json:"line_index"`
	PurchaseDate       string             `json:"purchase_date"`
	Description        string             `json:"description"`
	NormalizedMerchant string             `json:"normalized_merchant"`
	InstallmentCurrent *int               `json:"installment_current"`
	InstallmentTotal   *int               `json:"installment_total"`
	AmountCents        int64              `json:"amount_cents"`
	IsIgnored          bool               `json:"is_ignored"`
	SuggestedPersonID  *int64             `json:"suggested_person_id"`
	CreatedAt          string             `json:"created_at"`
	Splits             []TransactionSplit `json:"splits"`
}

type PersonTotal struct {
	PersonID   int64  `json:"person_id"`
	PersonName string `json:"person_name"`
	IsSelf     bool   `json:"is_self"`
	Color      string `json:"color"`
	TotalCents int64  `json:"total_cents"`
}

type StatementDetail struct {
	Statement            Statement     `json:"statement"`
	Transactions         []Transaction `json:"transactions"`
	TotalsByPerson       []PersonTotal `json:"totals_by_person"`
	UnassignedTotalCents int64         `json:"unassigned_total_cents"`
}

type AssignmentRule struct {
	ID                 int64  `json:"id"`
	NormalizedMerchant string `json:"normalized_merchant"`
	PersonID           int64  `json:"person_id"`
	MatchCount         int    `json:"match_count"`
	UpdatedAt          string `json:"updated_at"`
}

type Payment struct {
	ID             int64  `json:"id"`
	PersonID       int64  `json:"person_id"`
	ReferenceYear  int    `json:"reference_year"`
	ReferenceMonth int    `json:"reference_month"`
	AmountCents    int64  `json:"amount_cents"`
	PaidAt         string `json:"paid_at"`
	Note           string `json:"note"`
	CreatedAt      string `json:"created_at"`
}

type PaymentInput struct {
	PersonID       int64  `json:"person_id"`
	ReferenceYear  int    `json:"reference_year"`
	ReferenceMonth int    `json:"reference_month"`
	AmountCents    int64  `json:"amount_cents"`
	PaidAt         string `json:"paid_at"`
	Note           string `json:"note"`
}

type PersonDebtSummary struct {
	PersonID        int64  `json:"person_id"`
	PersonName      string `json:"person_name"`
	Email           string `json:"email"`
	IsSelf          bool   `json:"is_self"`
	Color           string `json:"color"`
	Archived        bool   `json:"archived"`
	TotalSpentCents int64  `json:"total_spent_cents"`
	TotalPaidCents  int64  `json:"total_paid_cents"`
	BalanceCents    int64  `json:"balance_cents"`
}

type MonthLedgerRow struct {
	Year              int       `json:"year"`
	Month             int       `json:"month"`
	SpentCents        int64     `json:"spent_cents"`
	PaidCents         int64     `json:"paid_cents"`
	MonthBalanceCents int64     `json:"month_balance_cents"`
	Status            string    `json:"status"`
	Payments          []Payment `json:"payments"`
}

type PersonLedger struct {
	Person          Person           `json:"person"`
	TotalSpentCents int64            `json:"total_spent_cents"`
	TotalPaidCents  int64            `json:"total_paid_cents"`
	BalanceCents    int64            `json:"balance_cents"`
	Months          []MonthLedgerRow `json:"months"`
}

type BackupReminderStatus struct {
	NeedsBackup     bool   `json:"needs_backup"`
	LastBackupAt    string `json:"last_backup_at"`
	LastMutationAt  string `json:"last_mutation_at"`
	DaysSinceBackup *int   `json:"days_since_backup"`
}

type MonthDashboardSummary struct {
	Month                int           `json:"month"`
	TotalCents           int64         `json:"total_cents"`
	SelfTotalCents       int64         `json:"self_total_cents"`
	OthersTotalCents     int64         `json:"others_total_cents"`
	UnassignedTotalCents int64         `json:"unassigned_total_cents"`
	UnassignedCount      int           `json:"unassigned_count"`
	TransactionCount     int           `json:"transaction_count"`
	ClassifiedCount      int           `json:"classified_count"`
	StatementCount       int           `json:"statement_count"`
	TotalsByPerson       []PersonTotal `json:"totals_by_person"`
	Statements           []Statement   `json:"statements"`
}

type DashboardSummary struct {
	Year                 int                     `json:"year"`
	Month                int                     `json:"month"`
	Bank                 string                  `json:"bank"`
	AvailableYears       []int                   `json:"available_years"`
	AvailableBanks       []string                `json:"available_banks"`
	TotalCents           int64                   `json:"total_cents"`
	SelfTotalCents       int64                   `json:"self_total_cents"`
	OthersTotalCents     int64                   `json:"others_total_cents"`
	UnassignedTotalCents int64                   `json:"unassigned_total_cents"`
	UnassignedCount      int                     `json:"unassigned_count"`
	TotalsByPerson       []PersonTotal           `json:"totals_by_person"`
	DebtByPerson         []PersonDebtSummary     `json:"debt_by_person"`
	Statements           []Statement             `json:"statements"`
	Months               []MonthDashboardSummary `json:"months"`
	BackupReminder       BackupReminderStatus    `json:"backup_reminder"`
}

type ReceiptMonthAvailability struct {
	Month      int   `json:"month"`
	TotalCents int64 `json:"total_cents"`
	ItemCount  int   `json:"item_count"`
}

type ReceiptItem struct {
	PurchaseDate          string `json:"purchase_date"`
	Bank                  string `json:"bank"`
	CardLabel             string `json:"card_label"`
	Description           string `json:"description"`
	InstallmentCurrent    *int   `json:"installment_current"`
	InstallmentTotal      *int   `json:"installment_total"`
	TransactionTotalCents int64  `json:"transaction_total_cents"`
	PersonAmountCents     int64  `json:"person_amount_cents"`
	IsSplit               bool   `json:"is_split"`
}

type ReceiptPreview struct {
	PersonID            int64         `json:"person_id"`
	PersonName          string        `json:"person_name"`
	PersonEmail         string        `json:"person_email"`
	ReferenceYear       int           `json:"reference_year"`
	ReferenceMonth      int           `json:"reference_month"`
	BankFilter          string        `json:"bank_filter"`
	IncludePriorBalance bool          `json:"include_prior_balance"`
	Items               []ReceiptItem `json:"items"`
	SubtotalCents       int64         `json:"subtotal_cents"`
	PaidCents           int64         `json:"paid_cents"`
	PriorBalanceCents   int64         `json:"prior_balance_cents"`
	TotalCents          int64         `json:"total_cents"`
	Subject             string        `json:"subject"`
	HTMLBody            string        `json:"html_body"`
	WhatsAppText        string        `json:"whatsapp_text"`
}

type Receipt struct {
	ID                  int64   `json:"id"`
	PersonID            int64   `json:"person_id"`
	PersonName          string  `json:"person_name"`
	ReferenceYear       int     `json:"reference_year"`
	ReferenceMonth      int     `json:"reference_month"`
	BankFilter          string  `json:"bank_filter"`
	IncludePriorBalance bool    `json:"include_prior_balance"`
	RecipientEmail      string  `json:"recipient_email"`
	Subject             string  `json:"subject"`
	HTMLBody            string  `json:"html_body"`
	SubtotalCents       int64   `json:"subtotal_cents"`
	PaidCents           int64   `json:"paid_cents"`
	PriorBalanceCents   int64   `json:"prior_balance_cents"`
	TotalCents          int64   `json:"total_cents"`
	DeliveryStatus      string  `json:"delivery_status"`
	ErrorMessage        string  `json:"error_message"`
	SentAt              *string `json:"sent_at"`
	CreatedAt           string  `json:"created_at"`
}

type PreviewTransaction struct {
	LineIndex          int          `json:"line_index"`
	PurchaseDate       string       `json:"purchase_date"`
	Description        string       `json:"description"`
	NormalizedMerchant string       `json:"normalized_merchant"`
	InstallmentCurrent *int         `json:"installment_current"`
	InstallmentTotal   *int         `json:"installment_total"`
	AmountCents        int64        `json:"amount_cents"`
	IsIgnored          bool         `json:"is_ignored"`
	SuggestedPersonID  *int64       `json:"suggested_person_id"`
	Splits             []SplitInput `json:"splits"`
}

type DuplicateWarning struct {
	ExactFileMatch    *Statement  `json:"exact_file_match"`
	PeriodBankMatches []Statement `json:"period_bank_matches"`
}

type PreviewResult struct {
	Status             string               `json:"status"`
	StagingID          string               `json:"staging_id"`
	OriginalFilename   string               `json:"original_filename"`
	FileFormat         string               `json:"file_format"`
	ParserUsed         string               `json:"parser_used"`
	Bank               string               `json:"bank"`
	ReferenceYear      int                  `json:"reference_year"`
	ReferenceMonth     int                  `json:"reference_month"`
	DeclaredTotalCents *int64               `json:"declared_total_cents"`
	ComputedTotalCents int64                `json:"computed_total_cents"`
	Transactions       []PreviewTransaction `json:"transactions"`
	DuplicateWarning   *DuplicateWarning    `json:"duplicate_warning"`
	Reason             string               `json:"reason"`
	HasExtractableText bool                 `json:"has_extractable_text"`
	TextPreview        string               `json:"text_preview"`
}

type SettingsView struct {
	SMTPHost           string `json:"smtp_host"`
	SMTPPort           int    `json:"smtp_port"`
	SMTPUser           string `json:"smtp_user"`
	SMTPFromName       string `json:"smtp_from_name"`
	SMTPTLSMode        string `json:"smtp_tls_mode"`
	HasSMTPPass        bool   `json:"has_smtp_pass"`
	AIProvider         string `json:"ai_provider"`
	AIModel            string `json:"ai_model"`
	HasAIAPIKey        bool   `json:"has_ai_api_key"`
	BackupReminderDays int    `json:"backup_reminder_days"`
	LastBackupAt       string `json:"last_backup_at"`
	LastMutationAt     string `json:"last_mutation_at"`
}
