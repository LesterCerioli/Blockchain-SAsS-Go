package repositories

const postgresSchema = `
CREATE TABLE IF NOT EXISTS merchants (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    api_key_hash TEXT NOT NULL UNIQUE,
    api_key_prefix TEXT NOT NULL,
    webhook_url TEXT NOT NULL DEFAULT '',
    webhook_secret TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS payment_intents (
    id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(id),
    reference TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    amount_value TEXT NOT NULL,
    amount_decimals INTEGER NOT NULL,
    currency TEXT NOT NULL,
    crypto_amount_value TEXT NOT NULL,
    crypto_decimals INTEGER NOT NULL,
    crypto_currency TEXT NOT NULL,
    blockchain TEXT NOT NULL,
    network TEXT NOT NULL,
    deposit_address TEXT NOT NULL,
    status TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    tx_hash TEXT NOT NULL DEFAULT '',
    confirmations INTEGER NOT NULL DEFAULT 0,
    required_confirmations INTEGER NOT NULL DEFAULT 0,
    paid_amount_value TEXT,
    paid_amount_decimals INTEGER,
    fee_amount_value TEXT,
    fee_amount_decimals INTEGER,
    refund_tx_hash TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    risk_score INTEGER NOT NULL DEFAULT 0,
    idempotency_key TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_intents_idempotency
    ON payment_intents (merchant_id, idempotency_key) WHERE idempotency_key <> '';
CREATE INDEX IF NOT EXISTS idx_payment_intents_merchant ON payment_intents (merchant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_payment_intents_address ON payment_intents (blockchain, network, lower(deposit_address));
CREATE INDEX IF NOT EXISTS idx_payment_intents_status ON payment_intents (status, expires_at);

CREATE TABLE IF NOT EXISTS escrows (
    id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(id),
    payment_intent_id TEXT NOT NULL REFERENCES payment_intents(id),
    amount_value TEXT NOT NULL,
    amount_decimals INTEGER NOT NULL,
    crypto_amount_value TEXT NOT NULL,
    crypto_decimals INTEGER NOT NULL,
    currency TEXT NOT NULL,
    crypto_currency TEXT NOT NULL,
    blockchain TEXT NOT NULL,
    network TEXT NOT NULL,
    beneficiary TEXT NOT NULL,
    arbiter TEXT NOT NULL DEFAULT '',
    release_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    released_at TIMESTAMPTZ,
    refunded_at TIMESTAMPTZ,
    tx_hash TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_escrows_merchant ON escrows (merchant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_escrows_status ON escrows (status, release_at);

CREATE TABLE IF NOT EXISTS blockchain_transactions (
    id TEXT PRIMARY KEY,
    payment_intent_id TEXT NOT NULL DEFAULT '',
    merchant_id TEXT NOT NULL DEFAULT '',
    tx_hash TEXT NOT NULL,
    blockchain TEXT NOT NULL,
    network TEXT NOT NULL,
    from_address TEXT NOT NULL DEFAULT '',
    to_address TEXT NOT NULL DEFAULT '',
    amount_value TEXT,
    amount_decimals INTEGER,
    fee_value TEXT,
    fee_decimals INTEGER,
    status TEXT NOT NULL,
    direction TEXT NOT NULL,
    confirmations INTEGER NOT NULL DEFAULT 0,
    block_number BIGINT NOT NULL DEFAULT 0,
    block_hash TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_transactions_hash ON blockchain_transactions (blockchain, network, lower(tx_hash));
CREATE INDEX IF NOT EXISTS idx_transactions_payment ON blockchain_transactions (payment_intent_id);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(id),
    event_type TEXT NOT NULL,
    url TEXT NOT NULL,
    payload TEXT NOT NULL,
    status TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    status_code INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    next_attempt_at TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_webhooks_merchant ON webhook_deliveries (merchant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_webhooks_pending ON webhook_deliveries (status, next_attempt_at);

CREATE TABLE IF NOT EXISTS audit_logs (
    id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL DEFAULT '',
    actor TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    ip_address TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_merchant ON audit_logs (merchant_id, created_at DESC);
`
