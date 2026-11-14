package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/blockchain-saas/payment-gateway/internal/shared/logger"
	"github.com/blockchain-saas/payment-gateway/internal/shared/utils"
	apperrors "github.com/blockchain-saas/payment-gateway/pkg/errors"
	"github.com/google/uuid"
)

type PaymentServiceOptions struct {
	DefaultExpiry time.Duration
	MaxExpiry     time.Duration
}

type PaymentService struct {
	merchants    ports.MerchantRepository
	payments     ports.PaymentIntentRepository
	transactions ports.TransactionRepository
	audit        ports.AuditRepository
	registry     ports.BlockchainRegistry
	exchange     *ExchangeService
	validation   *ValidationService
	fraud        *FraudService
	events       EventPublisher
	clock        ports.Clock
	logger       logger.Logger
	options      PaymentServiceOptions
}

func NewPaymentService(
	merchants ports.MerchantRepository,
	payments ports.PaymentIntentRepository,
	transactions ports.TransactionRepository,
	audit ports.AuditRepository,
	registry ports.BlockchainRegistry,
	exchange *ExchangeService,
	validation *ValidationService,
	fraud *FraudService,
	events EventPublisher,
	clock ports.Clock,
	log logger.Logger,
	options PaymentServiceOptions,
) *PaymentService {
	if clock == nil {
		clock = ports.SystemClock{}
	}
	if log == nil {
		log = logger.NopLogger{}
	}
	if events == nil {
		events = NopEventPublisher{}
	}
	if options.DefaultExpiry <= 0 {
		options.DefaultExpiry = 15 * time.Minute
	}
	return &PaymentService{
		merchants: merchants, payments: payments, transactions: transactions, audit: audit,
		registry: registry, exchange: exchange, validation: validation, fraud: fraud, events: events,
		clock: clock, logger: log, options: options,
	}
}

type CreatePaymentInput struct {
	MerchantID     uuid.UUID
	Amount         string
	Currency       string
	CryptoCurrency string
	Blockchain     string
	Network        string
	Reference      string
	Description    string
	ExpiresIn      int
	Metadata       map[string]string
	IdempotencyKey string
	Actor          string
	IPAddress      string
	UserAgent      string
}

type RefundInput struct {
	DestinationAddress string
	Amount             string
	SignedTransaction  string
	TxHash             string
	Actor              string
	IPAddress          string
	UserAgent          string
}

func (s *PaymentService) Create(ctx context.Context, input CreatePaymentInput) (*entities.PaymentIntent, error) {
	merchant, err := s.merchants.GetByID(ctx, input.MerchantID)
	if err != nil {
		return nil, mapDomainError(err, "merchant not found")
	}
	if !merchant.CanAcceptPayments() {
		return nil, apperrors.Forbidden("merchant account is not active")
	}

	if input.IdempotencyKey != "" {
		existing, err := s.payments.FindByIdempotencyKey(ctx, input.MerchantID, input.IdempotencyKey)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, entities.ErrNotFound) {
			return nil, apperrors.Internal("failed to check idempotency key", err)
		}
	}

	fiatCurrency := utils.NormalizeCurrency(input.Currency)
	cryptoCurrency := utils.NormalizeCurrency(input.CryptoCurrency)
	cryptoMode := false

	if cryptoCurrency == "" && s.validation.IsCryptoCurrency(fiatCurrency) {
		cryptoCurrency = fiatCurrency
		cryptoMode = true
	}
	if cryptoCurrency == "" {
		chainHint := input.Blockchain
		if chainHint == "" {
			chainHint = "ethereum"
		}
		chain, chainErr := utils.NormalizeBlockchain(chainHint)
		if chainErr != nil {
			return nil, apperrors.Validation(chainErr.Error(), nil)
		}
		cryptoCurrency = utils.NativeSymbol(chain)
	}

	chain, network, err := s.validation.ResolveChainNetwork(input.Blockchain, input.Network)
	if err != nil {
		if strings.TrimSpace(input.Blockchain) == "" {
			inferred, inferErr := utils.NormalizeBlockchain(inferChain(cryptoCurrency))
			if inferErr == nil {
				chain = inferred
				network, err = utils.NormalizeNetwork(chain, input.Network)
			}
		}
		if err != nil {
			return nil, apperrors.Validation(err.Error(), nil)
		}
	}

	adapter, err := s.registry.Get(chain, network)
	if err != nil {
		return nil, mapDomainError(err, "blockchain adapter not available")
	}
	info := adapter.Info()

	cryptoSymbol := strings.ToUpper(cryptoCurrency)
	cryptoDecimals, ok := utils.GetCurrencyDecimals(cryptoSymbol)
	if !ok {
		cryptoDecimals = info.Decimals
	}

	now := s.clock.Now()
	payment := entities.NewPaymentIntent(merchant.ID, now)
	payment.Reference = strings.TrimSpace(input.Reference)
	payment.Description = strings.TrimSpace(input.Description)
	payment.Blockchain = chain
	payment.Network = network
	payment.CryptoCurrency = cryptoSymbol
	payment.RequiredConfirmations = info.RequiredConfirmations
	if payment.RequiredConfirmations <= 0 {
		payment.RequiredConfirmations = 1
	}
	if input.Metadata != nil {
		payment.Metadata = input.Metadata
	}
	payment.IdempotencyKey = input.IdempotencyKey

	var amountUSD float64
	if cryptoMode {
		cryptoAmount, parseErr := s.validation.ParseAmount(input.Amount, cryptoSymbol)
		if parseErr != nil {
			return nil, apperrors.Validation(parseErr.Error(), nil)
		}
		payment.Amount = cryptoAmount
		payment.Currency = cryptoSymbol
		payment.CryptoAmount = cryptoAmount
		amountUSD = s.usdValue(ctx, cryptoAmount, cryptoSymbol)
	} else {
		fiatAmount, parseErr := s.validation.ParseAmount(input.Amount, fiatCurrency)
		if parseErr != nil {
			return nil, apperrors.Validation(parseErr.Error(), nil)
		}
		cryptoAmount, rate, convertErr := s.exchange.ConvertFiatToCrypto(ctx, fiatAmount, fiatCurrency, cryptoSymbol, cryptoDecimals)
		if convertErr != nil {
			return nil, apperrors.Unprocessable(fmt.Sprintf("unable to convert %s to %s: %v", fiatCurrency, cryptoSymbol, convertErr))
		}
		payment.Amount = fiatAmount
		payment.Currency = fiatCurrency
		payment.CryptoAmount = cryptoAmount
		payment.Metadata["exchange_rate"] = fmt.Sprintf("%.8f", rate.Rate)
		payment.Metadata["exchange_source"] = rate.Source
		amountUSD = s.usdValue(ctx, fiatAmount, fiatCurrency)
	}
	if payment.CryptoAmount == nil || payment.CryptoAmount.IsZero() {
		return nil, apperrors.Validation("crypto amount would be zero, please increase the amount", nil)
	}

	expiry, err := s.validation.Expiry(input.ExpiresIn, s.options.DefaultExpiry)
	if err != nil {
		return nil, apperrors.Validation(err.Error(), nil)
	}
	if s.options.MaxExpiry > 0 && expiry > s.options.MaxExpiry {
		return nil, apperrors.Validation("expiration exceeds maximum allowed", nil)
	}
	payment.ExpiresAt = now.Add(expiry)

	address, err := adapter.GenerateAddress(ctx, payment.ID.String())
	if err != nil {
		return nil, mapDomainError(err, "failed to generate deposit address")
	}
	payment.DepositAddress = address

	if s.fraud != nil {
		assessment, fraudErr := s.fraud.Assess(ctx, merchant.ID, amountUSD, chain, network, address)
		if fraudErr != nil {
			s.logger.Warn(ctx, "fraud assessment failed", "error", fraudErr)
		} else if assessment != nil {
			payment.RiskScore = assessment.Score
			if assessment.Blocked {
				return nil, apperrors.WithDetails(
					apperrors.Forbidden("payment blocked by risk engine"),
					map[string]interface{}{"reasons": assessment.Reasons, "risk_score": assessment.Score},
				)
			}
			if assessment.Review {
				if err := payment.MarkUnderReview(assessment.Score, now); err != nil {
					return nil, apperrors.Internal("failed to set risk review", err)
				}
				payment.Metadata["risk_reasons"] = strings.Join(assessment.Reasons, "; ")
			}
		}
	}

	if err := s.payments.Create(ctx, payment); err != nil {
		if errors.Is(err, entities.ErrConflict) && input.IdempotencyKey != "" {
			if existing, findErr := s.payments.FindByIdempotencyKey(ctx, input.MerchantID, input.IdempotencyKey); findErr == nil {
				return existing, nil
			}
		}
		return nil, mapDomainError(err, "failed to create payment intent")
	}

	s.auditLog(ctx, payment, input.Actor, "payment_intent.created", input.IPAddress, input.UserAgent, map[string]interface{}{
		"amount": payment.Amount.ToDecimalString(), "currency": payment.Currency,
		"crypto_amount": payment.CryptoAmount.ToDecimalString(), "crypto_currency": payment.CryptoCurrency,
	})
	s.publish(ctx, payment, entities.EventPaymentIntentCreated)
	return payment, nil
}

func (s *PaymentService) Get(ctx context.Context, merchantID, paymentID uuid.UUID) (*entities.PaymentIntent, error) {
	payment, err := s.payments.GetByID(ctx, paymentID)
	if err != nil {
		return nil, mapDomainError(err, "payment intent not found")
	}
	if merchantID != uuid.Nil && payment.MerchantID != merchantID {
		return nil, apperrors.NotFound("payment intent not found")
	}
	return payment, nil
}

func (s *PaymentService) List(ctx context.Context, filter ports.PaymentFilter) ([]*entities.PaymentIntent, int, error) {
	return s.payments.List(ctx, filter)
}

func (s *PaymentService) Confirm(ctx context.Context, merchantID, paymentID uuid.UUID, txHash, actor, ip, userAgent string) (*entities.PaymentIntent, error) {
	txHash = strings.TrimSpace(txHash)
	if txHash == "" {
		return nil, apperrors.Validation("transaction hash is required", nil)
	}
	payment, err := s.Get(ctx, merchantID, paymentID)
	if err != nil {
		return nil, err
	}
	if payment.Status == entities.PaymentStatusSucceeded {
		return payment, nil
	}
	if payment.IsFinal() {
		return nil, apperrors.Conflict(fmt.Sprintf("payment intent is %s", payment.Status))
	}
	if payment.IsExpired(s.clock.Now()) && payment.TxHash == "" {
		return nil, apperrors.Unprocessable("payment intent has expired")
	}

	adapter, err := s.registry.Get(payment.Blockchain, payment.Network)
	if err != nil {
		return nil, mapDomainError(err, "blockchain adapter not available")
	}

	receipt, err := adapter.GetTransactionStatus(ctx, txHash)
	if err != nil {
		return nil, apperrors.ServiceUnavailable("failed to fetch transaction from chain", err)
	}
	if receipt == nil || receipt.Status == types.StatusPending || receipt.Confirmations <= 0 {
		now := s.clock.Now()
		if payment.TxHash == "" {
			if err := payment.MarkProcessing(txHash, now); err != nil {
				return nil, mapDomainError(err, "invalid payment state")
			}
			if err := s.recordTransaction(ctx, payment, receipt, txHash); err != nil {
				s.logger.Warn(ctx, "failed to record pending transaction", "error", err)
			}
			if err := s.payments.Update(ctx, payment); err != nil {
				return nil, apperrors.Internal("failed to update payment intent", err)
			}
			s.auditLog(ctx, payment, actor, "payment_intent.processing", ip, userAgent, map[string]interface{}{"tx_hash": txHash})
			s.publish(ctx, payment, entities.EventPaymentIntentUpdated)
		}
		return payment, nil
	}

	verified, err := adapter.VerifyTransaction(ctx, txHash, payment.DepositAddress, payment.CryptoAmount)
	if err != nil {
		return nil, apperrors.ServiceUnavailable("failed to verify transaction", err)
	}
	if !verified {
		return nil, apperrors.Validation("transaction does not match the payment intent", map[string]interface{}{
			"expected_address": payment.DepositAddress,
			"expected_amount":  payment.CryptoAmount.ToDecimalString(),
		})
	}
	return s.applyReceipt(ctx, payment, receipt, actor, ip, userAgent)
}

func (s *PaymentService) HandleIncoming(ctx context.Context, incoming ports.IncomingTransaction) (*entities.PaymentIntent, error) {
	if incoming.TxHash == "" || incoming.To == "" || incoming.Amount == nil {
		return nil, apperrors.Validation("incomplete incoming transaction", nil)
	}
	exists, err := s.transactions.ExistsByHash(ctx, incoming.Blockchain, incoming.Network, incoming.TxHash)
	if err != nil {
		return nil, apperrors.Internal("failed to check transaction", err)
	}
	if exists {
		return nil, nil
	}

	candidates, err := s.payments.ListByDepositAddress(ctx, incoming.Blockchain, incoming.Network, incoming.To)
	if err != nil {
		return nil, apperrors.Internal("failed to find payment intent", err)
	}
	var target *entities.PaymentIntent
	for _, candidate := range candidates {
		if candidate.CryptoAmount == nil {
			continue
		}
		if incoming.Amount.Cmp(candidate.CryptoAmount) < 0 {
			continue
		}
		if candidate.IsExpired(s.clock.Now()) {
			continue
		}
		target = candidate
		break
	}
	if target == nil {
		return nil, fmt.Errorf("%w: no matching payment intent for address %s", entities.ErrNotFound, incoming.To)
	}

	receipt := &ports.TransactionReceipt{
		TxHash:        incoming.TxHash,
		From:          incoming.From,
		To:            incoming.To,
		Amount:        incoming.Amount,
		Fee:           incoming.Fee,
		Status:        incoming.Status,
		Confirmations: incoming.Confirmations,
		BlockNumber:   incoming.BlockNumber,
		BlockHash:     incoming.BlockHash,
		Timestamp:     incoming.Timestamp,
	}
	return s.applyReceipt(ctx, target, receipt, "blockchain-listener", "", "system")
}

func (s *PaymentService) RefreshPending(ctx context.Context) (int, error) {
	pending, err := s.payments.ListPendingConfirmation(ctx, 100)
	if err != nil {
		return 0, err
	}
	updated := 0
	for _, payment := range pending {
		adapter, err := s.registry.Get(payment.Blockchain, payment.Network)
		if err != nil {
			s.logger.Warn(ctx, "adapter unavailable while refreshing payment", "payment_id", payment.ID, "error", err)
			continue
		}
		receipt, err := adapter.GetTransactionStatus(ctx, payment.TxHash)
		if err != nil {
			s.logger.Warn(ctx, "failed to refresh payment", "payment_id", payment.ID, "error", err)
			continue
		}
		if receipt == nil {
			continue
		}
		if _, err := s.applyReceipt(ctx, payment, receipt, "confirmation-worker", "", "system"); err != nil {
			s.logger.Warn(ctx, "failed to apply receipt", "payment_id", payment.ID, "error", err)
			continue
		}
		updated++
	}
	return updated, nil
}

func (s *PaymentService) ExpireStale(ctx context.Context) (int, error) {
	expirable, err := s.payments.ListExpirable(ctx, s.clock.Now(), 200)
	if err != nil {
		return 0, err
	}
	expired := 0
	for _, payment := range expirable {
		if err := payment.Expire(s.clock.Now()); err != nil {
			continue
		}
		if err := s.payments.Update(ctx, payment); err != nil {
			s.logger.Warn(ctx, "failed to expire payment", "payment_id", payment.ID, "error", err)
			continue
		}
		s.auditLog(ctx, payment, "expiration-worker", "payment_intent.expired", "", "system", nil)
		s.publish(ctx, payment, entities.EventPaymentIntentExpired)
		expired++
	}
	return expired, nil
}

func (s *PaymentService) Cancel(ctx context.Context, merchantID, paymentID uuid.UUID, actor, ip, userAgent string) (*entities.PaymentIntent, error) {
	payment, err := s.Get(ctx, merchantID, paymentID)
	if err != nil {
		return nil, err
	}
	if payment.IsFinal() {
		return nil, apperrors.Conflict(fmt.Sprintf("payment intent is %s", payment.Status))
	}
	if payment.TxHash != "" || payment.Status == entities.PaymentStatusProcessing ||
		payment.Status == entities.PaymentStatusRequiresConfirmation {
		return nil, apperrors.Conflict("cannot cancel a payment that already received funds")
	}
	if err := payment.Cancel(s.clock.Now()); err != nil {
		return nil, mapDomainError(err, "invalid payment state")
	}
	if err := s.payments.Update(ctx, payment); err != nil {
		return nil, apperrors.Internal("failed to cancel payment intent", err)
	}
	s.auditLog(ctx, payment, actor, "payment_intent.canceled", ip, userAgent, nil)
	s.publish(ctx, payment, entities.EventPaymentIntentCanceled)
	return payment, nil
}

func (s *PaymentService) Refund(ctx context.Context, merchantID, paymentID uuid.UUID, input RefundInput) (*entities.PaymentIntent, error) {
	payment, err := s.Get(ctx, merchantID, paymentID)
	if err != nil {
		return nil, err
	}
	if payment.Status != entities.PaymentStatusSucceeded {
		return nil, apperrors.Unprocessable("only succeeded payments can be refunded")
	}
	adapter, err := s.registry.Get(payment.Blockchain, payment.Network)
	if err != nil {
		return nil, mapDomainError(err, "blockchain adapter not available")
	}

	amount := payment.PaidAmount
	if amount == nil {
		amount = payment.CryptoAmount
	}
	if input.Amount != "" {
		parsed, parseErr := s.validation.ParseAmount(input.Amount, payment.CryptoCurrency)
		if parseErr != nil {
			return nil, apperrors.Validation(parseErr.Error(), nil)
		}
		amount = parsed
	}

	txHash := strings.TrimSpace(input.TxHash)
	if txHash == "" {
		if input.DestinationAddress != "" {
			valid, validErr := adapter.ValidateAddress(ctx, input.DestinationAddress)
			if validErr != nil || !valid {
				return nil, apperrors.Validation("invalid refund destination address", nil)
			}
		}
		txHash, err = adapter.SendTransaction(ctx, ports.SendTransactionRequest{
			RawTransaction: input.SignedTransaction,
			To:             input.DestinationAddress,
			Amount:         amount,
			Priority:       types.PriorityMedium,
			Reference:      "refund:" + payment.ID.String(),
		})
		if err != nil {
			return nil, apperrors.ServiceUnavailable("failed to broadcast refund transaction", err)
		}
	}
	if err := payment.MarkRefunded(txHash, s.clock.Now()); err != nil {
		return nil, mapDomainError(err, "invalid payment state")
	}
	if err := s.payments.Update(ctx, payment); err != nil {
		return nil, apperrors.Internal("failed to persist refund", err)
	}
	s.auditLog(ctx, payment, input.Actor, "payment_intent.refunded", input.IPAddress, input.UserAgent, map[string]interface{}{
		"refund_tx_hash": txHash,
	})
	s.publish(ctx, payment, entities.EventPaymentIntentRefunded)
	return payment, nil
}

func (s *PaymentService) applyReceipt(ctx context.Context, payment *entities.PaymentIntent, receipt *ports.TransactionReceipt, actor, ip, userAgent string) (*entities.PaymentIntent, error) {
	now := s.clock.Now()
	if receipt.Status == types.StatusFailed {
		if err := payment.MarkFailed(now); err != nil {
			return nil, mapDomainError(err, "invalid payment state")
		}
		if err := s.recordTransaction(ctx, payment, receipt, receipt.TxHash); err != nil {
			s.logger.Warn(ctx, "failed to record transaction", "error", err)
		}
		if err := s.payments.Update(ctx, payment); err != nil {
			return nil, apperrors.Internal("failed to update payment intent", err)
		}
		s.auditLog(ctx, payment, actor, "payment_intent.failed", ip, userAgent, map[string]interface{}{"tx_hash": receipt.TxHash})
		s.publish(ctx, payment, entities.EventPaymentIntentFailed)
		return payment, nil
	}

	if payment.CryptoAmount != nil && receipt.Amount != nil && receipt.Amount.Cmp(payment.CryptoAmount) < 0 {
		payment.TxHash = receipt.TxHash
		payment.PaidAmount = receipt.Amount
		payment.FeeAmount = receipt.Fee
		payment.Confirmations = receipt.Confirmations
		payment.UpdatedAt = now
		if payment.Status == entities.PaymentStatusRequiresPayment || payment.Status == entities.PaymentStatusUnderReview {
			if err := payment.MarkProcessing(receipt.TxHash, now); err != nil {
				return nil, mapDomainError(err, "invalid payment state")
			}
		}
		if err := s.recordTransaction(ctx, payment, receipt, receipt.TxHash); err != nil {
			s.logger.Warn(ctx, "failed to record transaction", "error", err)
		}
		if err := s.payments.Update(ctx, payment); err != nil {
			return nil, apperrors.Internal("failed to update payment intent", err)
		}
		s.publish(ctx, payment, entities.EventPaymentIntentUpdated)
		return payment, nil
	}

	required := payment.RequiredConfirmations
	if required <= 0 {
		required = 1
	}
	if err := payment.ApplyConfirmation(receipt.TxHash, receipt.Confirmations, receipt.Amount, receipt.Fee, required, now); err != nil {
		return nil, mapDomainError(err, "invalid payment state")
	}
	if err := s.recordTransaction(ctx, payment, receipt, receipt.TxHash); err != nil {
		s.logger.Warn(ctx, "failed to record transaction", "error", err)
	}
	if err := s.payments.Update(ctx, payment); err != nil {
		return nil, apperrors.Internal("failed to update payment intent", err)
	}

	action := "payment_intent.updated"
	eventType := entities.EventPaymentIntentUpdated
	if payment.Status == entities.PaymentStatusSucceeded {
		action = "payment_intent.succeeded"
		eventType = entities.EventPaymentIntentSucceeded
	}
	s.auditLog(ctx, payment, actor, action, ip, userAgent, map[string]interface{}{
		"tx_hash": receipt.TxHash, "confirmations": receipt.Confirmations,
	})
	s.publish(ctx, payment, eventType)
	return payment, nil
}

func (s *PaymentService) recordTransaction(ctx context.Context, payment *entities.PaymentIntent, receipt *ports.TransactionReceipt, txHash string) error {
	if txHash == "" || s.transactions == nil {
		return nil
	}
	existing, err := s.transactions.GetByHash(ctx, payment.Blockchain, payment.Network, txHash)
	if err != nil && !errors.Is(err, entities.ErrNotFound) {
		return err
	}
	status := types.StatusPending
	confirmations := 0
	blockNumber := int64(0)
	blockHash := ""
	var amount, fee *types.Amount
	if receipt != nil {
		status = receipt.Status
		confirmations = receipt.Confirmations
		blockNumber = receipt.BlockNumber
		blockHash = receipt.BlockHash
		amount = receipt.Amount
		fee = receipt.Fee
	}
	if existing != nil {
		existing.Status = status
		existing.Confirmations = confirmations
		existing.BlockNumber = blockNumber
		existing.BlockHash = blockHash
		if amount != nil {
			existing.Amount = amount
		}
		if fee != nil {
			existing.Fee = fee
		}
		existing.UpdatedAt = s.clock.Now()
		return s.transactions.Update(ctx, existing)
	}
	tx := entities.NewBlockchainTransaction(s.clock.Now())
	tx.PaymentIntentID = payment.ID
	tx.MerchantID = payment.MerchantID
	tx.TxHash = txHash
	tx.Blockchain = payment.Blockchain
	tx.Network = payment.Network
	tx.Amount = amount
	tx.Fee = fee
	tx.Status = status
	tx.Direction = types.DirectionIncoming
	tx.Confirmations = confirmations
	tx.BlockNumber = blockNumber
	tx.BlockHash = blockHash
	if receipt != nil {
		tx.FromAddress = receipt.From
		tx.ToAddress = receipt.To
	}
	return s.transactions.Create(ctx, tx)
}

func (s *PaymentService) usdValue(ctx context.Context, amount *types.Amount, currency string) float64 {
	if amount == nil {
		return 0
	}
	currency = utils.NormalizeCurrency(currency)
	if currency == "USD" || currency == "USDT" || currency == "USDC" || currency == "DAI" {
		return amount.ToFloat()
	}
	rate, err := s.exchange.GetRate(ctx, currency, "USD")
	if err != nil || rate == nil || rate.Rate <= 0 {
		return 0
	}
	return amount.ToFloat() * rate.Rate
}

func (s *PaymentService) auditLog(ctx context.Context, payment *entities.PaymentIntent, actor, action, ip, userAgent string, details map[string]interface{}) {
	if s.audit == nil {
		return
	}
	if details == nil {
		details = map[string]interface{}{}
	}
	details["status"] = string(payment.Status)
	entry := entities.NewAuditLog(s.clock.Now())
	entry.MerchantID = payment.MerchantID
	entry.Actor = actor
	entry.Action = action
	entry.ResourceType = "payment_intent"
	entry.ResourceID = payment.ID.String()
	entry.Details = details
	entry.IPAddress = ip
	entry.UserAgent = userAgent
	if err := s.audit.Append(ctx, entry); err != nil {
		s.logger.Warn(ctx, "failed to append audit log", "error", err)
	}
}

func (s *PaymentService) publish(ctx context.Context, payment *entities.PaymentIntent, eventType entities.EventType) {
	if s.events == nil {
		return
	}
	event := entities.NewEvent(eventType, "payment_intent", payment.ID, payment.MerchantID, s.clock.Now(), paymentEventData(payment))
	if err := s.events.PublishEvent(ctx, event); err != nil {
		s.logger.Warn(ctx, "failed to publish payment event", "event_type", eventType, "error", err)
	}
}

func paymentEventData(payment *entities.PaymentIntent) map[string]interface{} {
	amount := ""
	if payment.Amount != nil {
		amount = payment.Amount.ToDecimalString()
	}
	cryptoAmount := ""
	if payment.CryptoAmount != nil {
		cryptoAmount = payment.CryptoAmount.ToDecimalString()
	}
	return map[string]interface{}{
		"payment_intent_id": payment.ID.String(),
		"merchant_id":       payment.MerchantID.String(),
		"status":            string(payment.Status),
		"amount":            amount,
		"currency":          payment.Currency,
		"crypto_amount":     cryptoAmount,
		"crypto_currency":   payment.CryptoCurrency,
		"blockchain":        string(payment.Blockchain),
		"network":           string(payment.Network),
		"deposit_address":   payment.DepositAddress,
		"tx_hash":           payment.TxHash,
		"reference":         payment.Reference,
		"expires_at":        payment.ExpiresAt.Format(time.RFC3339),
	}
}

func inferChain(currency string) string {
	switch strings.ToUpper(currency) {
	case "BTC":
		return "bitcoin"
	case "SOL":
		return "solana"
	case "ADA":
		return "cardano"
	case "ETH", "USDT", "USDC", "DAI":
		return "ethereum"
	case "BNB":
		return "bsc"
	case "MATIC":
		return "polygon"
	default:
		return "ethereum"
	}
}
