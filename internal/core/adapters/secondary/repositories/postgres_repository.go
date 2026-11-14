package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(ctx context.Context, dsn string, maxOpen, maxIdle int, connMaxLifetime time.Duration) (*PostgresStore, error) {
	if dsn == "" {
		return nil, errors.New("postgres dsn is empty")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if maxOpen > 0 {
		db.SetMaxOpenConns(maxOpen)
	}
	if maxIdle > 0 {
		db.SetMaxIdleConns(maxIdle)
	}
	if connMaxLifetime > 0 {
		db.SetConnMaxLifetime(connMaxLifetime)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &PostgresStore{db: db}, nil
}

func (s *PostgresStore) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, postgresSchema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

func (s *PostgresStore) Repositories() ports.Repositories {
	return ports.Repositories{
		Merchants:    &PostgresMerchantRepository{db: s.db},
		Payments:     &PostgresPaymentRepository{db: s.db},
		Escrows:      &PostgresEscrowRepository{db: s.db},
		Transactions: &PostgresTransactionRepository{db: s.db},
		Webhooks:     &PostgresWebhookRepository{db: s.db},
		Audit:        &PostgresAuditRepository{db: s.db},
	}
}

type scanner interface {
	Scan(dest ...any) error
}

func encodeAmount(a *types.Amount) (string, int) {
	if a == nil || a.Value == nil {
		return "0", 0
	}
	return a.Value.String(), a.Decimals
}

func nullableAmount(a *types.Amount) (any, any) {
	if a == nil || a.Value == nil {
		return nil, nil
	}
	return a.Value.String(), a.Decimals
}

func decodeAmount(value sql.NullString, decimals sql.NullInt64) *types.Amount {
	if !value.Valid || !decimals.Valid {
		return nil
	}
	amount, err := types.NewAmount(value.String, int(decimals.Int64))
	if err != nil {
		return nil
	}
	return amount
}

func jsonStringMap(m map[string]string) []byte {
	if m == nil {
		m = map[string]string{}
	}
	b, _ := json.Marshal(m)
	return b
}

func parseStringMap(data []byte) map[string]string {
	out := map[string]string{}
	if len(data) > 0 {
		_ = json.Unmarshal(data, &out)
	}
	return out
}

func jsonAnyMap(m map[string]interface{}) []byte {
	if m == nil {
		m = map[string]interface{}{}
	}
	b, _ := json.Marshal(m)
	return b
}

func parseAnyMap(data []byte) map[string]interface{} {
	out := map[string]interface{}{}
	if len(data) > 0 {
		_ = json.Unmarshal(data, &out)
	}
	return out
}

type PostgresMerchantRepository struct{ db *sql.DB }

const merchantColumns = `id, name, email, api_key_hash, api_key_prefix, webhook_url, webhook_secret, status, created_at, updated_at`

func scanMerchant(row scanner) (*entities.Merchant, error) {
	var merchant entities.Merchant
	var id string
	if err := row.Scan(&id, &merchant.Name, &merchant.Email, &merchant.APIKeyHash,
		&merchant.APIKeyPrefix, &merchant.WebhookURL, &merchant.WebhookSecret,
		&merchant.Status, &merchant.CreatedAt, &merchant.UpdatedAt); err != nil {
		return nil, err
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	merchant.ID = parsed
	return &merchant, nil
}

func (r *PostgresMerchantRepository) Create(ctx context.Context, merchant *entities.Merchant) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO merchants (`+merchantColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		merchant.ID.String(), merchant.Name, merchant.Email, merchant.APIKeyHash,
		merchant.APIKeyPrefix, merchant.WebhookURL, merchant.WebhookSecret, merchant.Status,
		merchant.CreatedAt, merchant.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return fmt.Errorf("%w: merchant", entities.ErrConflict)
		}
		return fmt.Errorf("create merchant: %w", err)
	}
	return nil
}

func (r *PostgresMerchantRepository) GetByID(ctx context.Context, id uuid.UUID) (*entities.Merchant, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+merchantColumns+` FROM merchants WHERE id=$1`, id.String())
	merchant, err := scanMerchant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: merchant %s", entities.ErrNotFound, id)
	}
	return merchant, err
}

func (r *PostgresMerchantRepository) GetByEmail(ctx context.Context, email string) (*entities.Merchant, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+merchantColumns+` FROM merchants WHERE lower(email)=lower($1)`, email)
	merchant, err := scanMerchant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: merchant with email %s", entities.ErrNotFound, email)
	}
	return merchant, err
}

func (r *PostgresMerchantRepository) GetByAPIKeyHash(ctx context.Context, hash string) (*entities.Merchant, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+merchantColumns+` FROM merchants WHERE api_key_hash=$1`, hash)
	merchant, err := scanMerchant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: merchant for api key", entities.ErrNotFound)
	}
	return merchant, err
}

func (r *PostgresMerchantRepository) GetByAPIKeyPrefix(ctx context.Context, prefix string) (*entities.Merchant, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+merchantColumns+` FROM merchants WHERE api_key_prefix=$1`, prefix)
	merchant, err := scanMerchant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: merchant for api key prefix", entities.ErrNotFound)
	}
	return merchant, err
}

func (r *PostgresMerchantRepository) Update(ctx context.Context, merchant *entities.Merchant) error {
	result, err := r.db.ExecContext(ctx, `UPDATE merchants SET name=$2,email=$3,api_key_hash=$4,api_key_prefix=$5,
		webhook_url=$6,webhook_secret=$7,status=$8,updated_at=$9 WHERE id=$1`,
		merchant.ID.String(), merchant.Name, merchant.Email, merchant.APIKeyHash,
		merchant.APIKeyPrefix, merchant.WebhookURL, merchant.WebhookSecret, merchant.Status,
		merchant.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update merchant: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: merchant %s", entities.ErrNotFound, merchant.ID)
	}
	return nil
}

func (r *PostgresMerchantRepository) List(ctx context.Context, pagination ports.Pagination) ([]*entities.Merchant, int, error) {
	limit, offset := normalizePagination(pagination)
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM merchants`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+merchantColumns+` FROM merchants ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := make([]*entities.Merchant, 0)
	for rows.Next() {
		merchant, err := scanMerchant(rows)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, merchant)
	}
	return result, total, rows.Err()
}

type PostgresPaymentRepository struct{ db *sql.DB }

const paymentColumns = `id, merchant_id, reference, description, amount_value, amount_decimals, currency,
	crypto_amount_value, crypto_decimals, crypto_currency, blockchain, network, deposit_address, status,
	expires_at, tx_hash, confirmations, required_confirmations, paid_amount_value, paid_amount_decimals,
	fee_amount_value, fee_amount_decimals, refund_tx_hash, metadata, risk_score, idempotency_key,
	created_at, updated_at, completed_at`

func scanPayment(row scanner) (*entities.PaymentIntent, error) {
	var p entities.PaymentIntent
	var id, merchantID string
	var amountValue, cryptoValue string
	var amountDecimals, cryptoDecimals int
	var paidValue, feeValue sql.NullString
	var paidDecimals, feeDecimals sql.NullInt64
	var metadata []byte
	var completedAt sql.NullTime
	if err := row.Scan(&id, &merchantID, &p.Reference, &p.Description, &amountValue, &amountDecimals,
		&p.Currency, &cryptoValue, &cryptoDecimals, &p.CryptoCurrency, &p.Blockchain, &p.Network,
		&p.DepositAddress, &p.Status, &p.ExpiresAt, &p.TxHash, &p.Confirmations, &p.RequiredConfirmations,
		&paidValue, &paidDecimals, &feeValue, &feeDecimals, &p.RefundTxHash, &metadata, &p.RiskScore,
		&p.IdempotencyKey, &p.CreatedAt, &p.UpdatedAt, &completedAt); err != nil {
		return nil, err
	}
	parsedID, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	parsedMerchantID, err := uuid.Parse(merchantID)
	if err != nil {
		return nil, err
	}
	p.ID = parsedID
	p.MerchantID = parsedMerchantID
	p.Amount, _ = types.NewAmount(amountValue, amountDecimals)
	p.CryptoAmount, _ = types.NewAmount(cryptoValue, cryptoDecimals)
	p.PaidAmount = decodeAmount(paidValue, paidDecimals)
	p.FeeAmount = decodeAmount(feeValue, feeDecimals)
	p.Metadata = parseStringMap(metadata)
	if completedAt.Valid {
		t := completedAt.Time
		p.CompletedAt = &t
	}
	return &p, nil
}

func (r *PostgresPaymentRepository) Create(ctx context.Context, p *entities.PaymentIntent) error {
	amountValue, amountDecimals := encodeAmount(p.Amount)
	cryptoValue, cryptoDecimals := encodeAmount(p.CryptoAmount)
	paidValue, paidDecimals := nullableAmount(p.PaidAmount)
	feeValue, feeDecimals := nullableAmount(p.FeeAmount)
	_, err := r.db.ExecContext(ctx, `INSERT INTO payment_intents (`+paymentColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29)`,
		p.ID.String(), p.MerchantID.String(), p.Reference, p.Description, amountValue, amountDecimals,
		p.Currency, cryptoValue, cryptoDecimals, p.CryptoCurrency, p.Blockchain, p.Network,
		p.DepositAddress, p.Status, p.ExpiresAt, p.TxHash, p.Confirmations, p.RequiredConfirmations,
		paidValue, paidDecimals, feeValue, feeDecimals, p.RefundTxHash, jsonStringMap(p.Metadata),
		p.RiskScore, p.IdempotencyKey, p.CreatedAt, p.UpdatedAt, p.CompletedAt)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return fmt.Errorf("%w: payment intent or idempotency key", entities.ErrConflict)
		}
		return fmt.Errorf("create payment intent: %w", err)
	}
	return nil
}

func (r *PostgresPaymentRepository) GetByID(ctx context.Context, id uuid.UUID) (*entities.PaymentIntent, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+paymentColumns+` FROM payment_intents WHERE id=$1`, id.String())
	p, err := scanPayment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: payment intent %s", entities.ErrNotFound, id)
	}
	return p, err
}

func (r *PostgresPaymentRepository) Update(ctx context.Context, p *entities.PaymentIntent) error {
	amountValue, amountDecimals := encodeAmount(p.Amount)
	cryptoValue, cryptoDecimals := encodeAmount(p.CryptoAmount)
	paidValue, paidDecimals := nullableAmount(p.PaidAmount)
	feeValue, feeDecimals := nullableAmount(p.FeeAmount)
	result, err := r.db.ExecContext(ctx, `UPDATE payment_intents SET merchant_id=$2,reference=$3,description=$4,
		amount_value=$5,amount_decimals=$6,currency=$7,crypto_amount_value=$8,crypto_decimals=$9,
		crypto_currency=$10,blockchain=$11,network=$12,deposit_address=$13,status=$14,expires_at=$15,
		tx_hash=$16,confirmations=$17,required_confirmations=$18,paid_amount_value=$19,
		paid_amount_decimals=$20,fee_amount_value=$21,fee_amount_decimals=$22,refund_tx_hash=$23,
		metadata=$24,risk_score=$25,idempotency_key=$26,created_at=$27,updated_at=$28,completed_at=$29
		WHERE id=$1`,
		p.ID.String(), p.MerchantID.String(), p.Reference, p.Description, amountValue, amountDecimals,
		p.Currency, cryptoValue, cryptoDecimals, p.CryptoCurrency, p.Blockchain, p.Network,
		p.DepositAddress, p.Status, p.ExpiresAt, p.TxHash, p.Confirmations, p.RequiredConfirmations,
		paidValue, paidDecimals, feeValue, feeDecimals, p.RefundTxHash, jsonStringMap(p.Metadata),
		p.RiskScore, p.IdempotencyKey, p.CreatedAt, p.UpdatedAt, p.CompletedAt)
	if err != nil {
		return fmt.Errorf("update payment intent: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: payment intent %s", entities.ErrNotFound, p.ID)
	}
	return nil
}

func (r *PostgresPaymentRepository) FindByIdempotencyKey(ctx context.Context, merchantID uuid.UUID, key string) (*entities.PaymentIntent, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+paymentColumns+` FROM payment_intents WHERE merchant_id=$1 AND idempotency_key=$2`,
		merchantID.String(), key)
	p, err := scanPayment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: payment intent with idempotency key", entities.ErrNotFound)
	}
	return p, err
}

func (r *PostgresPaymentRepository) List(ctx context.Context, filter ports.PaymentFilter) ([]*entities.PaymentIntent, int, error) {
	var conditions []string
	var args []any
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if filter.MerchantID != uuid.Nil {
		conditions = append(conditions, "merchant_id="+arg(filter.MerchantID.String()))
	}
	if filter.Blockchain != "" {
		conditions = append(conditions, "blockchain="+arg(string(filter.Blockchain)))
	}
	if filter.Network != "" {
		conditions = append(conditions, "network="+arg(string(filter.Network)))
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			placeholders = append(placeholders, arg(string(status)))
		}
		conditions = append(conditions, "status IN ("+strings.Join(placeholders, ",")+")")
	}
	if filter.From != nil {
		conditions = append(conditions, "created_at >= "+arg(*filter.From))
	}
	if filter.To != nil {
		conditions = append(conditions, "created_at <= "+arg(*filter.To))
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM payment_intents`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	order := "created_at DESC"
	if filter.SortBy == "amount" {
		order = "amount_value::numeric DESC"
	}
	if strings.EqualFold(filter.SortOrder, "asc") {
		order = strings.Replace(order, "DESC", "ASC", 1)
	}
	limit, offset := normalizePagination(filter.Pagination)
	args = append(args, limit, offset)
	query := `SELECT ` + paymentColumns + ` FROM payment_intents` + where +
		` ORDER BY ` + order + fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := make([]*entities.PaymentIntent, 0)
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, p)
	}
	return result, total, rows.Err()
}

func (r *PostgresPaymentRepository) ListByDepositAddress(ctx context.Context, blockchain types.BlockchainType, network types.BlockchainNetwork, address string) ([]*entities.PaymentIntent, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+paymentColumns+` FROM payment_intents
		WHERE blockchain=$1 AND network=$2 AND lower(deposit_address)=lower($3)
		AND status IN ('requires_payment','under_review','processing','requires_confirmation')
		ORDER BY created_at ASC`, string(blockchain), string(network), address)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*entities.PaymentIntent, 0)
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (r *PostgresPaymentRepository) ListPendingConfirmation(ctx context.Context, limit int) ([]*entities.PaymentIntent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+paymentColumns+` FROM payment_intents
		WHERE tx_hash <> '' AND status IN ('processing','requires_confirmation')
		ORDER BY updated_at ASC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*entities.PaymentIntent, 0)
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (r *PostgresPaymentRepository) ListExpirable(ctx context.Context, now time.Time, limit int) ([]*entities.PaymentIntent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+paymentColumns+` FROM payment_intents
		WHERE status IN ('requires_payment','under_review') AND expires_at <= $1
		ORDER BY expires_at ASC LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*entities.PaymentIntent, 0)
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

type PostgresEscrowRepository struct{ db *sql.DB }

const escrowColumns = `id, merchant_id, payment_intent_id, amount_value, amount_decimals,
	crypto_amount_value, crypto_decimals, currency, crypto_currency, blockchain, network,
	beneficiary, arbiter, release_at, status, released_at, refunded_at, tx_hash, created_at, updated_at`

func scanEscrow(row scanner) (*entities.Escrow, error) {
	var e entities.Escrow
	var id, merchantID, paymentIntentID string
	var amountValue, cryptoValue string
	var amountDecimals, cryptoDecimals int
	var releasedAt, refundedAt sql.NullTime
	if err := row.Scan(&id, &merchantID, &paymentIntentID, &amountValue, &amountDecimals,
		&cryptoValue, &cryptoDecimals, &e.Currency, &e.CryptoCurrency, &e.Blockchain, &e.Network,
		&e.Beneficiary, &e.Arbiter, &e.ReleaseAt, &e.Status, &releasedAt, &refundedAt,
		&e.TxHash, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return nil, err
	}
	e.ID, _ = uuid.Parse(id)
	e.MerchantID, _ = uuid.Parse(merchantID)
	e.PaymentIntentID, _ = uuid.Parse(paymentIntentID)
	e.Amount, _ = types.NewAmount(amountValue, amountDecimals)
	e.CryptoAmount, _ = types.NewAmount(cryptoValue, cryptoDecimals)
	if releasedAt.Valid {
		t := releasedAt.Time
		e.ReleasedAt = &t
	}
	if refundedAt.Valid {
		t := refundedAt.Time
		e.RefundedAt = &t
	}
	return &e, nil
}

func (r *PostgresEscrowRepository) Create(ctx context.Context, e *entities.Escrow) error {
	amountValue, amountDecimals := encodeAmount(e.Amount)
	cryptoValue, cryptoDecimals := encodeAmount(e.CryptoAmount)
	_, err := r.db.ExecContext(ctx, `INSERT INTO escrows (`+escrowColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		e.ID.String(), e.MerchantID.String(), e.PaymentIntentID.String(), amountValue, amountDecimals,
		cryptoValue, cryptoDecimals, e.Currency, e.CryptoCurrency, e.Blockchain, e.Network,
		e.Beneficiary, e.Arbiter, e.ReleaseAt, e.Status, e.ReleasedAt, e.RefundedAt, e.TxHash,
		e.CreatedAt, e.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return fmt.Errorf("%w: escrow", entities.ErrConflict)
		}
		return fmt.Errorf("create escrow: %w", err)
	}
	return nil
}

func (r *PostgresEscrowRepository) GetByID(ctx context.Context, id uuid.UUID) (*entities.Escrow, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+escrowColumns+` FROM escrows WHERE id=$1`, id.String())
	e, err := scanEscrow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: escrow %s", entities.ErrNotFound, id)
	}
	return e, err
}

func (r *PostgresEscrowRepository) Update(ctx context.Context, e *entities.Escrow) error {
	amountValue, amountDecimals := encodeAmount(e.Amount)
	cryptoValue, cryptoDecimals := encodeAmount(e.CryptoAmount)
	result, err := r.db.ExecContext(ctx, `UPDATE escrows SET merchant_id=$2,payment_intent_id=$3,
		amount_value=$4,amount_decimals=$5,crypto_amount_value=$6,crypto_decimals=$7,currency=$8,
		crypto_currency=$9,blockchain=$10,network=$11,beneficiary=$12,arbiter=$13,release_at=$14,
		status=$15,released_at=$16,refunded_at=$17,tx_hash=$18,updated_at=$19 WHERE id=$1`,
		e.ID.String(), e.MerchantID.String(), e.PaymentIntentID.String(), amountValue, amountDecimals,
		cryptoValue, cryptoDecimals, e.Currency, e.CryptoCurrency, e.Blockchain, e.Network,
		e.Beneficiary, e.Arbiter, e.ReleaseAt, e.Status, e.ReleasedAt, e.RefundedAt, e.TxHash, e.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update escrow: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: escrow %s", entities.ErrNotFound, e.ID)
	}
	return nil
}

func (r *PostgresEscrowRepository) ListByMerchant(ctx context.Context, merchantID uuid.UUID, pagination ports.Pagination) ([]*entities.Escrow, int, error) {
	limit, offset := normalizePagination(pagination)
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM escrows WHERE merchant_id=$1`, merchantID.String()).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+escrowColumns+` FROM escrows WHERE merchant_id=$1
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`, merchantID.String(), limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := make([]*entities.Escrow, 0)
	for rows.Next() {
		e, err := scanEscrow(rows)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, e)
	}
	return result, total, rows.Err()
}

func (r *PostgresEscrowRepository) ListReleasable(ctx context.Context, now time.Time, limit int) ([]*entities.Escrow, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+escrowColumns+` FROM escrows
		WHERE status='funded' AND release_at <= $1 ORDER BY release_at ASC LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*entities.Escrow, 0)
	for rows.Next() {
		e, err := scanEscrow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

type PostgresTransactionRepository struct{ db *sql.DB }

const txColumns = `id, payment_intent_id, merchant_id, tx_hash, blockchain, network, from_address,
	to_address, amount_value, amount_decimals, fee_value, fee_decimals, status, direction,
	confirmations, block_number, block_hash, metadata, created_at, updated_at`

func scanTransaction(row scanner) (*entities.BlockchainTransaction, error) {
	var t entities.BlockchainTransaction
	var id, paymentID, merchantID string
	var amountValue, feeValue sql.NullString
	var amountDecimals, feeDecimals sql.NullInt64
	var metadata []byte
	if err := row.Scan(&id, &paymentID, &merchantID, &t.TxHash, &t.Blockchain, &t.Network,
		&t.FromAddress, &t.ToAddress, &amountValue, &amountDecimals, &feeValue, &feeDecimals,
		&t.Status, &t.Direction, &t.Confirmations, &t.BlockNumber, &t.BlockHash, &metadata,
		&t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	t.ID, _ = uuid.Parse(id)
	if paymentID != "" {
		t.PaymentIntentID, _ = uuid.Parse(paymentID)
	}
	if merchantID != "" {
		t.MerchantID, _ = uuid.Parse(merchantID)
	}
	t.Amount = decodeAmount(amountValue, amountDecimals)
	t.Fee = decodeAmount(feeValue, feeDecimals)
	t.Metadata = parseStringMap(metadata)
	return &t, nil
}

func (r *PostgresTransactionRepository) Create(ctx context.Context, t *entities.BlockchainTransaction) error {
	amountValue, amountDecimals := nullableAmount(t.Amount)
	feeValue, feeDecimals := nullableAmount(t.Fee)
	paymentID := ""
	if t.PaymentIntentID != uuid.Nil {
		paymentID = t.PaymentIntentID.String()
	}
	merchantID := ""
	if t.MerchantID != uuid.Nil {
		merchantID = t.MerchantID.String()
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO blockchain_transactions (`+txColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		t.ID.String(), paymentID, merchantID, t.TxHash, t.Blockchain, t.Network, t.FromAddress,
		t.ToAddress, amountValue, amountDecimals, feeValue, feeDecimals, t.Status, t.Direction,
		t.Confirmations, t.BlockNumber, t.BlockHash, jsonStringMap(t.Metadata), t.CreatedAt, t.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return fmt.Errorf("%w: transaction %s", entities.ErrConflict, t.TxHash)
		}
		return fmt.Errorf("create transaction: %w", err)
	}
	return nil
}

func (r *PostgresTransactionRepository) Update(ctx context.Context, t *entities.BlockchainTransaction) error {
	amountValue, amountDecimals := nullableAmount(t.Amount)
	feeValue, feeDecimals := nullableAmount(t.Fee)
	result, err := r.db.ExecContext(ctx, `UPDATE blockchain_transactions SET payment_intent_id=$2,
		merchant_id=$3,from_address=$4,to_address=$5,amount_value=$6,amount_decimals=$7,
		fee_value=$8,fee_decimals=$9,status=$10,direction=$11,confirmations=$12,block_number=$13,
		block_hash=$14,metadata=$15,updated_at=$16
		WHERE blockchain=$17 AND network=$18 AND lower(tx_hash)=lower($19)`,
		t.ID.String(), uuidOrEmpty(t.PaymentIntentID), uuidOrEmpty(t.MerchantID), t.FromAddress,
		t.ToAddress, amountValue, amountDecimals, feeValue, feeDecimals, t.Status, t.Direction,
		t.Confirmations, t.BlockNumber, t.BlockHash, jsonStringMap(t.Metadata), t.UpdatedAt,
		string(t.Blockchain), string(t.Network), t.TxHash)
	if err != nil {
		return fmt.Errorf("update transaction: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: transaction %s", entities.ErrNotFound, t.TxHash)
	}
	return nil
}

func uuidOrEmpty(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func (r *PostgresTransactionRepository) GetByHash(ctx context.Context, blockchain types.BlockchainType, network types.BlockchainNetwork, hash string) (*entities.BlockchainTransaction, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+txColumns+` FROM blockchain_transactions
		WHERE blockchain=$1 AND network=$2 AND lower(tx_hash)=lower($3)`,
		string(blockchain), string(network), hash)
	t, err := scanTransaction(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: transaction %s", entities.ErrNotFound, hash)
	}
	return t, err
}

func (r *PostgresTransactionRepository) ExistsByHash(ctx context.Context, blockchain types.BlockchainType, network types.BlockchainNetwork, hash string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM blockchain_transactions
		WHERE blockchain=$1 AND network=$2 AND lower(tx_hash)=lower($3))`,
		string(blockchain), string(network), hash).Scan(&exists)
	return exists, err
}

func (r *PostgresTransactionRepository) ListByPayment(ctx context.Context, paymentIntentID uuid.UUID) ([]*entities.BlockchainTransaction, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+txColumns+` FROM blockchain_transactions
		WHERE payment_intent_id=$1 ORDER BY created_at ASC`, paymentIntentID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*entities.BlockchainTransaction, 0)
	for rows.Next() {
		t, err := scanTransaction(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

type PostgresWebhookRepository struct{ db *sql.DB }

const webhookColumns = `id, merchant_id, event_type, url, payload, status, attempts, status_code,
	last_error, next_attempt_at, metadata, created_at, updated_at`

func scanWebhook(row scanner) (*ports.WebhookDelivery, error) {
	var w ports.WebhookDelivery
	var id, merchantID string
	var nextAttempt sql.NullTime
	var metadata []byte
	if err := row.Scan(&id, &merchantID, &w.EventType, &w.URL, &w.Payload, &w.Status,
		&w.Attempts, &w.StatusCode, &w.LastError, &nextAttempt, &metadata, &w.CreatedAt, &w.UpdatedAt); err != nil {
		return nil, err
	}
	w.ID, _ = uuid.Parse(id)
	w.MerchantID, _ = uuid.Parse(merchantID)
	if nextAttempt.Valid {
		t := nextAttempt.Time
		w.NextAttemptAt = &t
	}
	w.Metadata = parseAnyMap(metadata)
	return &w, nil
}

func (r *PostgresWebhookRepository) Create(ctx context.Context, w *ports.WebhookDelivery) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO webhook_deliveries (`+webhookColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		w.ID.String(), w.MerchantID.String(), w.EventType, w.URL, w.Payload, w.Status,
		w.Attempts, w.StatusCode, w.LastError, w.NextAttemptAt, jsonAnyMap(w.Metadata),
		w.CreatedAt, w.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create webhook delivery: %w", err)
	}
	return nil
}

func (r *PostgresWebhookRepository) Update(ctx context.Context, w *ports.WebhookDelivery) error {
	result, err := r.db.ExecContext(ctx, `UPDATE webhook_deliveries SET event_type=$2,url=$3,payload=$4,
		status=$5,attempts=$6,status_code=$7,last_error=$8,next_attempt_at=$9,metadata=$10,updated_at=$11
		WHERE id=$1`,
		w.ID.String(), w.EventType, w.URL, w.Payload, w.Status, w.Attempts, w.StatusCode,
		w.LastError, w.NextAttemptAt, jsonAnyMap(w.Metadata), w.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update webhook delivery: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: webhook delivery %s", entities.ErrNotFound, w.ID)
	}
	return nil
}

func (r *PostgresWebhookRepository) GetByID(ctx context.Context, id uuid.UUID) (*ports.WebhookDelivery, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+webhookColumns+` FROM webhook_deliveries WHERE id=$1`, id.String())
	w, err := scanWebhook(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: webhook delivery %s", entities.ErrNotFound, id)
	}
	return w, err
}

func (r *PostgresWebhookRepository) ListByMerchant(ctx context.Context, merchantID uuid.UUID, pagination ports.Pagination) ([]*ports.WebhookDelivery, int, error) {
	limit, offset := normalizePagination(pagination)
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM webhook_deliveries WHERE merchant_id=$1`, merchantID.String()).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+webhookColumns+` FROM webhook_deliveries
		WHERE merchant_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, merchantID.String(), limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := make([]*ports.WebhookDelivery, 0)
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, w)
	}
	return result, total, rows.Err()
}

func (r *PostgresWebhookRepository) ListPending(ctx context.Context, now time.Time, limit int) ([]*ports.WebhookDelivery, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+webhookColumns+` FROM webhook_deliveries
		WHERE status IN ('pending','retrying') AND (next_attempt_at IS NULL OR next_attempt_at <= $1)
		ORDER BY created_at ASC LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*ports.WebhookDelivery, 0)
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, w)
	}
	return result, rows.Err()
}

type PostgresAuditRepository struct{ db *sql.DB }

func (r *PostgresAuditRepository) Append(ctx context.Context, entry *entities.AuditLog) error {
	merchantID := ""
	if entry.MerchantID != uuid.Nil {
		merchantID = entry.MerchantID.String()
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO audit_logs
		(id,merchant_id,actor,action,resource_type,resource_id,details,ip_address,user_agent,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		entry.ID.String(), merchantID, entry.Actor, entry.Action, entry.ResourceType,
		entry.ResourceID, jsonAnyMap(entry.Details), entry.IPAddress, entry.UserAgent, entry.CreatedAt)
	if err != nil {
		return fmt.Errorf("append audit log: %w", err)
	}
	return nil
}

func (r *PostgresAuditRepository) ListByMerchant(ctx context.Context, merchantID uuid.UUID, pagination ports.Pagination) ([]*entities.AuditLog, int, error) {
	limit, offset := normalizePagination(pagination)
	where := ""
	args := []any{}
	if merchantID != uuid.Nil {
		where = " WHERE merchant_id=$1"
		args = append(args, merchantID.String())
	}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM audit_logs`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	query := `SELECT id,merchant_id,actor,action,resource_type,resource_id,details,ip_address,user_agent,created_at
		FROM audit_logs` + where + fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := make([]*entities.AuditLog, 0)
	for rows.Next() {
		var entry entities.AuditLog
		var id, merchant string
		var details []byte
		if err := rows.Scan(&id, &merchant, &entry.Actor, &entry.Action, &entry.ResourceType,
			&entry.ResourceID, &details, &entry.IPAddress, &entry.UserAgent, &entry.CreatedAt); err != nil {
			return nil, 0, err
		}
		entry.ID, _ = uuid.Parse(id)
		if merchant != "" {
			entry.MerchantID, _ = uuid.Parse(merchant)
		}
		entry.Details = parseAnyMap(details)
		result = append(result, &entry)
	}
	return result, total, rows.Err()
}

func normalizePagination(pagination ports.Pagination) (int, int) {
	limit := pagination.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	offset := pagination.Offset
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
