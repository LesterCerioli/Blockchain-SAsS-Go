package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
	"github.com/blockchain-saas/payment-gateway/internal/shared/logger"
	"github.com/blockchain-saas/payment-gateway/internal/shared/monitoring"
	"github.com/blockchain-saas/payment-gateway/internal/shared/utils"
	"github.com/blockchain-saas/payment-gateway/pkg/api"
	apperrors "github.com/blockchain-saas/payment-gateway/pkg/errors"
	"github.com/google/uuid"
)

type HandlerDeps struct {
	Merchants            *services.MerchantService
	Payments             *services.PaymentService
	Escrows              *services.EscrowService
	Webhooks             *services.WebhookService
	Exchange             *services.ExchangeService
	Registry             ports.BlockchainRegistry
	Audit                ports.AuditRepository
	Health               *monitoring.HealthChecker
	Metrics              *monitoring.Registry
	InboundWebhookSecret string
	Clock                ports.Clock
	Logger               logger.Logger
}

type Handler struct {
	deps HandlerDeps
}

func NewHandler(deps HandlerDeps) *Handler {
	if deps.Clock == nil {
		deps.Clock = ports.SystemClock{}
	}
	if deps.Logger == nil {
		deps.Logger = logger.NopLogger{}
	}
	return &Handler{deps: deps}
}

func (h *Handler) Liveness(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, http.StatusOK, h.deps.Health.Liveness())
}

func (h *Handler) Readiness(w http.ResponseWriter, r *http.Request) {
	report := h.deps.Health.Readiness(r.Context())
	status := http.StatusOK
	if report.Status != "ok" {
		status = http.StatusServiceUnavailable
	}
	api.WriteJSON(w, status, report)
}

func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	h.deps.Metrics.Handler().ServeHTTP(w, r)
}

func (h *Handler) Chains(w http.ResponseWriter, r *http.Request) {
	api.WriteSuccess(w, r, http.StatusOK, h.deps.Registry.Supported())
}

func (h *Handler) Rate(w http.ResponseWriter, r *http.Request) {
	base := r.URL.Query().Get("base")
	quote := r.URL.Query().Get("quote")
	if base == "" || quote == "" {
		api.WriteError(w, r, apperrors.Validation("base and quote query parameters are required", nil))
		return
	}
	rate, err := h.deps.Exchange.GetRate(r.Context(), base, quote)
	if err != nil {
		api.WriteError(w, r, apperrors.ServiceUnavailable("failed to fetch exchange rate", err))
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, RateResponse{
		Base: rate.Base, Quote: rate.Quote, Rate: rate.Rate, Source: rate.Source, Timestamp: rate.Timestamp,
	})
}

func (h *Handler) RegisterMerchant(w http.ResponseWriter, r *http.Request) {
	var request RegisterMerchantRequest
	if err := decodeJSON(r, &request); err != nil {
		api.WriteError(w, r, err)
		return
	}
	merchant, apiKey, err := h.deps.Merchants.Register(r.Context(), services.RegisterMerchantInput{
		Name: request.Name, Email: request.Email, WebhookURL: request.WebhookURL,
		Actor: "public-signup", IPAddress: ClientIP(r), UserAgent: r.UserAgent(),
	})
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusCreated, RegisterMerchantResponse{
		Merchant: newMerchantResponse(merchant), APIKey: apiKey, WebhookSecret: merchant.WebhookSecret,
	})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var request LoginRequest
	if err := decodeJSON(r, &request); err != nil {
		api.WriteError(w, r, err)
		return
	}
	merchant, token, expires, err := h.deps.Merchants.LoginWithAPIKey(r.Context(), request.APIKey)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, TokenResponse{
		Token: token, TokenType: "Bearer", ExpiresAt: expires, Merchant: newMerchantResponse(merchant),
	})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	merchant, ok := MerchantFromContext(r.Context())
	if !ok {
		api.WriteError(w, r, apperrors.Unauthorized("authentication required"))
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, newMerchantResponse(merchant))
}

func (h *Handler) UpdateWebhook(w http.ResponseWriter, r *http.Request) {
	merchant, ok := MerchantFromContext(r.Context())
	if !ok {
		api.WriteError(w, r, apperrors.Unauthorized("authentication required"))
		return
	}
	var request UpdateWebhookRequest
	if err := decodeJSON(r, &request); err != nil {
		api.WriteError(w, r, err)
		return
	}
	updated, secret, err := h.deps.Merchants.UpdateWebhook(r.Context(), merchant.ID, request.WebhookURL, merchant.ID.String(), ClientIP(r), r.UserAgent())
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, UpdateWebhookResponse{Merchant: newMerchantResponse(updated), WebhookSecret: secret})
}

func (h *Handler) RotateKey(w http.ResponseWriter, r *http.Request) {
	merchant, ok := MerchantFromContext(r.Context())
	if !ok {
		api.WriteError(w, r, apperrors.Unauthorized("authentication required"))
		return
	}
	updated, apiKey, err := h.deps.Merchants.RotateAPIKey(r.Context(), merchant.ID, merchant.ID.String(), ClientIP(r), r.UserAgent())
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, RotateKeyResponse{Merchant: newMerchantResponse(updated), APIKey: apiKey})
}

func (h *Handler) CreatePaymentIntent(w http.ResponseWriter, r *http.Request) {
	merchant, ok := MerchantFromContext(r.Context())
	if !ok {
		api.WriteError(w, r, apperrors.Unauthorized("authentication required"))
		return
	}
	var request CreatePaymentIntentRequest
	if err := decodeJSON(r, &request); err != nil {
		api.WriteError(w, r, err)
		return
	}
	if request.MerchantID != "" && request.MerchantID != merchant.ID.String() {
		api.WriteError(w, r, apperrors.Forbidden("merchantId does not match the authenticated merchant"))
		return
	}
	payment, err := h.deps.Payments.Create(r.Context(), services.CreatePaymentInput{
		MerchantID: merchant.ID, Amount: request.Amount, Currency: request.Currency,
		CryptoCurrency: request.CryptoCurrency, Blockchain: request.Blockchain, Network: request.Network,
		Reference: request.Reference, Description: request.Description, ExpiresIn: request.ExpiresIn,
		Metadata: request.Metadata, IdempotencyKey: strings.TrimSpace(r.Header.Get("Idempotency-Key")),
		Actor: merchant.ID.String(), IPAddress: ClientIP(r), UserAgent: r.UserAgent(),
	})
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusCreated, newPaymentIntentResponse(payment))
}

func (h *Handler) ListPaymentIntents(w http.ResponseWriter, r *http.Request) {
	merchant, ok := MerchantFromContext(r.Context())
	if !ok {
		api.WriteError(w, r, apperrors.Unauthorized("authentication required"))
		return
	}
	pagination, page, pageSize := parsePagination(r)
	statuses := parseStatuses(r.URL.Query().Get("status"))
	filter := ports.PaymentFilter{
		MerchantID: merchant.ID, Statuses: statuses,
		Pagination: pagination,
		SortBy:     r.URL.Query().Get("sort_by"),
		SortOrder:  r.URL.Query().Get("sort_order"),
	}
	if from := parseTime(r.URL.Query().Get("from")); from != nil {
		filter.From = from
	}
	if to := parseTime(r.URL.Query().Get("to")); to != nil {
		filter.To = to
	}
	if chain := r.URL.Query().Get("blockchain"); chain != "" {
		resolved, err := utils.NormalizeBlockchain(chain)
		if err != nil {
			api.WriteError(w, r, apperrors.Validation(err.Error(), nil))
			return
		}
		filter.Blockchain = resolved
	}
	payments, total, err := h.deps.Payments.List(r.Context(), filter)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	response := make([]PaymentIntentResponse, 0, len(payments))
	for _, payment := range payments {
		response = append(response, newPaymentIntentResponse(payment))
	}
	api.WriteList(w, r, response, &api.Pagination{Page: page, PageSize: pageSize, Total: total})
}

func (h *Handler) GetPaymentIntent(w http.ResponseWriter, r *http.Request) {
	merchant, ok := MerchantFromContext(r.Context())
	if !ok {
		api.WriteError(w, r, apperrors.Unauthorized("authentication required"))
		return
	}
	paymentID, err := pathUUID(r)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	payment, err := h.deps.Payments.Get(r.Context(), merchant.ID, paymentID)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, newPaymentIntentResponse(payment))
}

func (h *Handler) ConfirmPaymentIntent(w http.ResponseWriter, r *http.Request) {
	merchant, ok := MerchantFromContext(r.Context())
	if !ok {
		api.WriteError(w, r, apperrors.Unauthorized("authentication required"))
		return
	}
	paymentID, err := pathUUID(r)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	var request ConfirmPaymentRequest
	if err := decodeJSON(r, &request); err != nil {
		api.WriteError(w, r, err)
		return
	}
	payment, err := h.deps.Payments.Confirm(r.Context(), merchant.ID, paymentID, request.TxHash, merchant.ID.String(), ClientIP(r), r.UserAgent())
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, newPaymentIntentResponse(payment))
}

func (h *Handler) CancelPaymentIntent(w http.ResponseWriter, r *http.Request) {
	merchant, ok := MerchantFromContext(r.Context())
	if !ok {
		api.WriteError(w, r, apperrors.Unauthorized("authentication required"))
		return
	}
	paymentID, err := pathUUID(r)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	payment, err := h.deps.Payments.Cancel(r.Context(), merchant.ID, paymentID, merchant.ID.String(), ClientIP(r), r.UserAgent())
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, newPaymentIntentResponse(payment))
}

func (h *Handler) RefundPaymentIntent(w http.ResponseWriter, r *http.Request) {
	merchant, ok := MerchantFromContext(r.Context())
	if !ok {
		api.WriteError(w, r, apperrors.Unauthorized("authentication required"))
		return
	}
	paymentID, err := pathUUID(r)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	var request RefundPaymentRequest
	if err := decodeOptionalJSON(r, &request); err != nil {
		api.WriteError(w, r, err)
		return
	}
	payment, err := h.deps.Payments.Refund(r.Context(), merchant.ID, paymentID, services.RefundInput{
		DestinationAddress: request.DestinationAddress, Amount: request.Amount,
		SignedTransaction: request.SignedTransaction, TxHash: request.TxHash,
		Actor: merchant.ID.String(), IPAddress: ClientIP(r), UserAgent: r.UserAgent(),
	})
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, newPaymentIntentResponse(payment))
}

func (h *Handler) CreateEscrow(w http.ResponseWriter, r *http.Request) {
	merchant, ok := MerchantFromContext(r.Context())
	if !ok {
		api.WriteError(w, r, apperrors.Unauthorized("authentication required"))
		return
	}
	var request CreateEscrowRequest
	if err := decodeJSON(r, &request); err != nil {
		api.WriteError(w, r, err)
		return
	}
	paymentID, err := uuid.Parse(request.PaymentIntentID)
	if err != nil {
		api.WriteError(w, r, apperrors.Validation("invalid payment_intent_id", nil))
		return
	}
	escrow, err := h.deps.Escrows.Create(r.Context(), services.CreateEscrowInput{
		MerchantID: merchant.ID, PaymentIntentID: paymentID, Beneficiary: request.Beneficiary,
		Arbiter: request.Arbiter, ReleaseAt: request.ReleaseAt,
		Actor: merchant.ID.String(), IPAddress: ClientIP(r), UserAgent: r.UserAgent(),
	})
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusCreated, newEscrowResponse(escrow))
}

func (h *Handler) ListEscrows(w http.ResponseWriter, r *http.Request) {
	merchant, ok := MerchantFromContext(r.Context())
	if !ok {
		api.WriteError(w, r, apperrors.Unauthorized("authentication required"))
		return
	}
	pagination, page, pageSize := parsePagination(r)
	escrows, total, err := h.deps.Escrows.List(r.Context(), merchant.ID, pagination)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	response := make([]EscrowResponse, 0, len(escrows))
	for _, escrow := range escrows {
		response = append(response, newEscrowResponse(escrow))
	}
	api.WriteList(w, r, response, &api.Pagination{Page: page, PageSize: pageSize, Total: total})
}

func (h *Handler) GetEscrow(w http.ResponseWriter, r *http.Request) {
	merchant, ok := MerchantFromContext(r.Context())
	if !ok {
		api.WriteError(w, r, apperrors.Unauthorized("authentication required"))
		return
	}
	escrowID, err := pathUUID(r)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	escrow, err := h.deps.Escrows.Get(r.Context(), merchant.ID, escrowID)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, newEscrowResponse(escrow))
}

func (h *Handler) ReleaseEscrow(w http.ResponseWriter, r *http.Request) {
	merchant, ok := MerchantFromContext(r.Context())
	if !ok {
		api.WriteError(w, r, apperrors.Unauthorized("authentication required"))
		return
	}
	escrowID, err := pathUUID(r)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	var request ExecuteEscrowRequest
	if err := decodeOptionalJSON(r, &request); err != nil {
		api.WriteError(w, r, err)
		return
	}
	escrow, err := h.deps.Escrows.Release(r.Context(), merchant.ID, escrowID, request.Force, services.EscrowExecutionInput{
		TxHash: request.TxHash, SignedTransaction: request.SignedTransaction,
		Actor: merchant.ID.String(), IPAddress: ClientIP(r), UserAgent: r.UserAgent(),
	})
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, newEscrowResponse(escrow))
}

func (h *Handler) RefundEscrow(w http.ResponseWriter, r *http.Request) {
	merchant, ok := MerchantFromContext(r.Context())
	if !ok {
		api.WriteError(w, r, apperrors.Unauthorized("authentication required"))
		return
	}
	escrowID, err := pathUUID(r)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	var request ExecuteEscrowRequest
	if err := decodeOptionalJSON(r, &request); err != nil {
		api.WriteError(w, r, err)
		return
	}
	escrow, err := h.deps.Escrows.Refund(r.Context(), merchant.ID, escrowID, services.EscrowExecutionInput{
		TxHash: request.TxHash, SignedTransaction: request.SignedTransaction,
		Actor: merchant.ID.String(), IPAddress: ClientIP(r), UserAgent: r.UserAgent(),
	})
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, newEscrowResponse(escrow))
}

func (h *Handler) CancelEscrow(w http.ResponseWriter, r *http.Request) {
	merchant, ok := MerchantFromContext(r.Context())
	if !ok {
		api.WriteError(w, r, apperrors.Unauthorized("authentication required"))
		return
	}
	escrowID, err := pathUUID(r)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	escrow, err := h.deps.Escrows.Cancel(r.Context(), merchant.ID, escrowID, merchant.ID.String(), ClientIP(r), r.UserAgent())
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, newEscrowResponse(escrow))
}

func (h *Handler) ListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	merchant, ok := MerchantFromContext(r.Context())
	if !ok {
		api.WriteError(w, r, apperrors.Unauthorized("authentication required"))
		return
	}
	pagination, page, pageSize := parsePagination(r)
	deliveries, total, err := h.deps.Webhooks.ListDeliveries(r.Context(), merchant.ID, pagination)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	response := make([]WebhookDeliveryResponse, 0, len(deliveries))
	for _, delivery := range deliveries {
		response = append(response, newWebhookDeliveryResponse(delivery))
	}
	api.WriteList(w, r, response, &api.Pagination{Page: page, PageSize: pageSize, Total: total})
}

func (h *Handler) TestWebhook(w http.ResponseWriter, r *http.Request) {
	merchant, ok := MerchantFromContext(r.Context())
	if !ok {
		api.WriteError(w, r, apperrors.Unauthorized("authentication required"))
		return
	}
	delivery, err := h.deps.Webhooks.SendTest(r.Context(), merchant.ID)
	if err != nil {
		api.WriteError(w, r, apperrors.ServiceUnavailable("webhook delivery failed", err))
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, newWebhookDeliveryResponse(delivery))
}

func (h *Handler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	merchant, ok := MerchantFromContext(r.Context())
	if !ok {
		api.WriteError(w, r, apperrors.Unauthorized("authentication required"))
		return
	}
	pagination, page, pageSize := parsePagination(r)
	logs, total, err := h.deps.Audit.ListByMerchant(r.Context(), merchant.ID, pagination)
	if err != nil {
		api.WriteError(w, r, apperrors.Internal("failed to list audit logs", err))
		return
	}
	api.WriteList(w, r, logs, &api.Pagination{Page: page, PageSize: pageSize, Total: total})
}

// InboundBlockchainWebhook accepts signed transaction notifications from
// external chain listeners or node providers.
func (h *Handler) InboundBlockchainWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		api.WriteError(w, r, apperrors.BadRequest("failed to read request body"))
		return
	}
	if h.deps.InboundWebhookSecret != "" {
		timestamp := r.Header.Get("X-Webhook-Timestamp")
		signature := r.Header.Get("X-Signature")
		if timestamp == "" || signature == "" ||
			utils.ValidateWebhookTimestamp(timestamp, h.deps.Clock.Now(), utils.DefaultWebhookTimestampTolerance) != nil ||
			!utils.VerifyWebhookSignature(h.deps.InboundWebhookSecret, timestamp, body, signature) {
			api.WriteError(w, r, apperrors.Unauthorized("invalid webhook signature"))
			return
		}
	}
	var request InboundWebhookRequest
	if err := json.Unmarshal(body, &request); err != nil {
		api.WriteError(w, r, apperrors.BadRequest("invalid JSON body"))
		return
	}
	chain, err := utils.NormalizeBlockchain(request.Blockchain)
	if err != nil {
		api.WriteError(w, r, apperrors.Validation(err.Error(), nil))
		return
	}
	network, err := utils.NormalizeNetwork(chain, request.Network)
	if err != nil {
		api.WriteError(w, r, apperrors.Validation(err.Error(), nil))
		return
	}
	adapter, err := h.deps.Registry.Get(chain, network)
	if err != nil {
		api.WriteError(w, r, apperrors.Unsupported(err.Error()))
		return
	}
	amount, err := types.NewAmountFromDecimal(request.Amount, adapter.Info().Decimals)
	if err != nil {
		api.WriteError(w, r, apperrors.Validation("invalid amount", nil))
		return
	}
	status := types.StatusPending
	if request.Status != "" {
		status = types.TransactionStatus(request.Status)
	} else if request.Confirmations >= adapter.Info().RequiredConfirmations {
		status = types.StatusConfirmed
	}
	payment, err := h.deps.Payments.HandleIncoming(r.Context(), ports.IncomingTransaction{
		Blockchain: chain, Network: network,
		TxHash: request.TxHash, From: request.From, To: request.To, Amount: amount,
		Status: status, Confirmations: request.Confirmations,
		BlockNumber: request.BlockNumber, BlockHash: request.BlockHash,
		Timestamp: h.deps.Clock.Now(),
	})
	if err != nil {
		if errors.Is(err, entities.ErrNotFound) {
			api.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"matched": false})
			return
		}
		api.WriteError(w, r, err)
		return
	}
	if payment == nil {
		api.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"matched": false, "duplicate": true})
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, newPaymentIntentResponse(payment))
}

func (h *Handler) AdminListMerchants(w http.ResponseWriter, r *http.Request) {
	pagination, page, pageSize := parsePagination(r)
	merchants, total, err := h.deps.Merchants.List(r.Context(), pagination)
	if err != nil {
		api.WriteError(w, r, err)
		return
	}
	response := make([]MerchantResponse, 0, len(merchants))
	for _, merchant := range merchants {
		response = append(response, newMerchantResponse(merchant))
	}
	api.WriteList(w, r, response, &api.Pagination{Page: page, PageSize: pageSize, Total: total})
}

func decodeJSON(r *http.Request, destination interface{}) error {
	if r.Body == nil {
		return apperrors.BadRequest("request body is required")
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if errors.Is(err, io.EOF) {
			return apperrors.BadRequest("request body is required")
		}
		return apperrors.BadRequest("invalid JSON body: " + err.Error())
	}
	return nil
}

func decodeOptionalJSON(r *http.Request, destination interface{}) error {
	if r.Body == nil {
		return nil
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return apperrors.BadRequest("invalid JSON body: " + err.Error())
	}
	return nil
}

func pathUUID(r *http.Request) (uuid.UUID, error) {
	raw := r.PathValue("id")
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperrors.Validation("invalid resource id", nil)
	}
	return id, nil
}

func parsePagination(r *http.Request) (ports.Pagination, int, int) {
	page := 1
	pageSize := 50
	if value := r.URL.Query().Get("page"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if value := r.URL.Query().Get("page_size"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 && parsed <= 500 {
			pageSize = parsed
		}
	}
	offset := (page - 1) * pageSize
	return ports.Pagination{Limit: pageSize, Offset: offset}, page, pageSize
}

func parseStatuses(value string) []entities.PaymentIntentStatus {
	parts := utils.SplitCSV(value)
	statuses := make([]entities.PaymentIntentStatus, 0, len(parts))
	for _, part := range parts {
		statuses = append(statuses, entities.PaymentIntentStatus(strings.ToLower(part)))
	}
	return statuses
}

func parseTime(value string) *time.Time {
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	return &parsed
}
