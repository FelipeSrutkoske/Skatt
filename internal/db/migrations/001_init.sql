CREATE TABLE people (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL UNIQUE,
    email       TEXT NOT NULL DEFAULT '',
    is_self     INTEGER NOT NULL DEFAULT 0,
    color       TEXT NOT NULL DEFAULT '#475569',
    archived    INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE statements (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    bank                 TEXT NOT NULL,
    card_label           TEXT NOT NULL DEFAULT '',
    reference_year       INTEGER NOT NULL,
    reference_month      INTEGER NOT NULL CHECK (reference_month BETWEEN 1 AND 12),
    original_filename    TEXT NOT NULL DEFAULT '',
    stored_path          TEXT NOT NULL DEFAULT '',
    file_sha256          TEXT NOT NULL DEFAULT '',
    file_format          TEXT NOT NULL,
    parser_used          TEXT NOT NULL,
    declared_total_cents INTEGER,
    imported_at          TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_statements_period_bank ON statements(reference_year, reference_month, bank);
CREATE INDEX idx_statements_sha256 ON statements(file_sha256);

CREATE TABLE transactions (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    statement_id        INTEGER NOT NULL REFERENCES statements(id) ON DELETE CASCADE,
    line_index          INTEGER NOT NULL,
    purchase_date       TEXT NOT NULL,
    description         TEXT NOT NULL,
    normalized_merchant TEXT NOT NULL,
    installment_current INTEGER,
    installment_total   INTEGER,
    amount_cents        INTEGER NOT NULL,
    is_ignored          INTEGER NOT NULL DEFAULT 0,
    suggested_person_id INTEGER REFERENCES people(id) ON DELETE SET NULL,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(statement_id, line_index)
);
CREATE INDEX idx_transactions_statement ON transactions(statement_id);
CREATE INDEX idx_transactions_merchant ON transactions(normalized_merchant);

CREATE TABLE transaction_splits (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    transaction_id INTEGER NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    person_id      INTEGER NOT NULL REFERENCES people(id) ON DELETE RESTRICT,
    amount_cents   INTEGER NOT NULL,
    UNIQUE(transaction_id, person_id)
);
CREATE INDEX idx_splits_person ON transaction_splits(person_id);

CREATE TABLE assignment_rules (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    normalized_merchant TEXT NOT NULL UNIQUE,
    person_id           INTEGER NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    match_count         INTEGER NOT NULL DEFAULT 1,
    updated_at          TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE payments (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    person_id       INTEGER NOT NULL REFERENCES people(id) ON DELETE RESTRICT,
    reference_year  INTEGER NOT NULL,
    reference_month INTEGER NOT NULL CHECK (reference_month BETWEEN 1 AND 12),
    amount_cents    INTEGER NOT NULL CHECK (amount_cents > 0),
    paid_at         TEXT NOT NULL,
    note            TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_payments_person_period ON payments(person_id, reference_year, reference_month);

CREATE TABLE receipts (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    person_id             INTEGER NOT NULL REFERENCES people(id) ON DELETE RESTRICT,
    reference_year        INTEGER NOT NULL,
    reference_month       INTEGER NOT NULL CHECK (reference_month BETWEEN 1 AND 12),
    bank_filter           TEXT NOT NULL DEFAULT '',
    include_prior_balance INTEGER NOT NULL DEFAULT 0,
    recipient_email       TEXT NOT NULL DEFAULT '',
    subject               TEXT NOT NULL,
    html_body             TEXT NOT NULL,
    subtotal_cents        INTEGER NOT NULL,
    paid_cents            INTEGER NOT NULL DEFAULT 0,
    prior_balance_cents   INTEGER NOT NULL DEFAULT 0,
    total_cents           INTEGER NOT NULL,
    delivery_status       TEXT NOT NULL,
    error_message         TEXT NOT NULL DEFAULT '',
    sent_at               TEXT,
    created_at            TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
