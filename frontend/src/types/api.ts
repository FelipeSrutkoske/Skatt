export interface Person {
  id: number
  name: string
  email: string
  is_self: boolean
  color: string
  archived: boolean
  created_at: string
}

export interface PersonInput {
  name: string
  email?: string
  color?: string
  archived?: boolean
}

export interface Statement {
  id: number
  bank: string
  card_label: string
  reference_year: number
  reference_month: number
  original_filename: string
  stored_path: string
  file_sha256: string
  file_format: string
  parser_used: string
  declared_total_cents: number | null
  imported_at: string
  total_cents: number
  transaction_count: number
  classified_count: number
  unassigned_count: number
}

export interface TransactionSplit {
  id: number
  transaction_id: number
  person_id: number
  person_name: string
  amount_cents: number
}

export interface SplitInput {
  person_id: number
  amount_cents: number
}

export interface Transaction {
  id: number
  statement_id: number
  line_index: number
  purchase_date: string
  description: string
  normalized_merchant: string
  installment_current: number | null
  installment_total: number | null
  amount_cents: number
  is_ignored: boolean
  suggested_person_id: number | null
  created_at: string
  splits: TransactionSplit[]
}

export interface UpdateTransactionRequest {
  purchase_date?: string
  description?: string
  amount_cents?: number
  is_ignored?: boolean
}

export interface PersonTotal {
  person_id: number
  person_name: string
  is_self: boolean
  color: string
  total_cents: number
}

export interface StatementDetail {
  statement: Statement
  transactions: Transaction[]
  totals_by_person: PersonTotal[]
  unassigned_total_cents: number
}

export interface AssignmentRule {
  id: number
  normalized_merchant: string
  person_id: number
  match_count: number
  updated_at: string
}

export interface Payment {
  id: number
  person_id: number
  reference_year: number
  reference_month: number
  amount_cents: number
  paid_at: string
  note: string
  created_at: string
}

export interface PaymentInput {
  person_id: number
  reference_year: number
  reference_month: number
  amount_cents: number
  paid_at: string
  note?: string
}

export interface PersonDebtSummary {
  person_id: number
  person_name: string
  email: string
  is_self: boolean
  color: string
  archived: boolean
  total_spent_cents: number
  total_paid_cents: number
  balance_cents: number
}

export interface MonthLedgerRow {
  year: number
  month: number
  spent_cents: number
  paid_cents: number
  month_balance_cents: number
  status: string
  payments: Payment[]
}

export interface PersonLedger {
  person: Person
  total_spent_cents: number
  total_paid_cents: number
  balance_cents: number
  months: MonthLedgerRow[]
}

export interface BackupReminderStatus {
  needs_backup: boolean
  last_backup_at: string
  last_mutation_at: string
  days_since_backup: number | null
}

export interface MonthDashboardSummary {
  month: number
  total_cents: number
  self_total_cents: number
  others_total_cents: number
  unassigned_total_cents: number
  unassigned_count: number
  transaction_count: number
  classified_count: number
  statement_count: number
  totals_by_person: PersonTotal[]
  statements: Statement[]
}

export interface DashboardSummary {
  year: number
  month: number
  bank: string
  available_years: number[]
  available_banks: string[]
  total_cents: number
  self_total_cents: number
  others_total_cents: number
  unassigned_total_cents: number
  unassigned_count: number
  totals_by_person: PersonTotal[]
  debt_by_person: PersonDebtSummary[]
  statements: Statement[]
  months: MonthDashboardSummary[]
  backup_reminder: BackupReminderStatus
}

export interface ReceiptMonthAvailability {
  month: number
  total_cents: number
  item_count: number
}

export interface ReceiptItem {
  purchase_date: string
  bank: string
  card_label: string
  description: string
  installment_current: number | null
  installment_total: number | null
  transaction_total_cents: number
  person_amount_cents: number
  is_split: boolean
}

export interface ReceiptPreview {
  person_id: number
  person_name: string
  person_email: string
  reference_year: number
  reference_month: number
  bank_filter: string
  include_prior_balance: boolean
  items: ReceiptItem[]
  subtotal_cents: number
  paid_cents: number
  prior_balance_cents: number
  total_cents: number
  subject: string
  html_body: string
  whatsapp_text: string
}

export interface Receipt {
  id: number
  person_id: number
  person_name: string
  reference_year: number
  reference_month: number
  bank_filter: string
  include_prior_balance: boolean
  recipient_email: string
  subject: string
  html_body: string
  subtotal_cents: number
  paid_cents: number
  prior_balance_cents: number
  total_cents: number
  delivery_status: string
  error_message: string
  sent_at: string | null
  created_at: string
}

export interface ReceiptPreviewRequest {
  person_id: number
  reference_year: number
  reference_month: number
  bank_filter?: string
  include_prior_balance?: boolean
}

export interface SendReceiptRequest {
  person_id: number
  reference_year: number
  reference_month: number
  bank_filter?: string
  include_prior_balance?: boolean
  recipient_email: string
  subject?: string
}

export interface PreviewTransaction {
  line_index: number
  purchase_date: string
  description: string
  normalized_merchant: string
  installment_current: number | null
  installment_total: number | null
  amount_cents: number
  is_ignored: boolean
  suggested_person_id: number | null
  splits: SplitInput[]
}

export interface DuplicateWarning {
  exact_file_match: Statement | null
  period_bank_matches: Statement[]
}

export type PreviewStatus = 'parsed' | 'needs_password' | 'needs_fallback'

export interface PreviewResult {
  status: PreviewStatus | string
  staging_id: string
  original_filename: string
  file_format: string
  parser_used: string
  bank: string
  reference_year: number
  reference_month: number
  declared_total_cents: number | null
  computed_total_cents: number
  transactions: PreviewTransaction[]
  duplicate_warning: DuplicateWarning | null
  reason: string
  has_extractable_text: boolean
  text_preview: string
}

export interface ConfirmImportTransactionInput {
  purchase_date: string
  description: string
  amount_cents: number
  is_ignored?: boolean
  splits?: SplitInput[]
}

export interface ConfirmImportRequest {
  staging_id?: string
  bank: string
  card_label?: string
  reference_year: number
  reference_month: number
  declared_total_cents?: number | null
  parser_used?: string
  replace_statement_id?: number | null
  transactions: ConfirmImportTransactionInput[]
}

export interface SettingsView {
  smtp_host: string
  smtp_port: number
  smtp_user: string
  smtp_from_name: string
  smtp_tls_mode: string
  has_smtp_pass: boolean
  ai_provider: string
  ai_model: string
  has_ai_api_key: boolean
  backup_reminder_days: number
  last_backup_at: string
  last_mutation_at: string
}

export interface UpdateSettingsRequest {
  smtp_host?: string
  smtp_port?: number
  smtp_user?: string
  smtp_pass?: string
  clear_smtp_pass?: boolean
  smtp_from_name?: string
  smtp_tls_mode?: string
  ai_provider?: string
  ai_model?: string
  ai_api_key?: string
  clear_ai_api_key?: boolean
  backup_reminder_days?: number
}

export interface SMTPTestRequest {
  to: string
}

export interface BatchAssignResult {
  updated: number
}

export interface OperationResult {
  ok?: boolean
  message?: string
}
