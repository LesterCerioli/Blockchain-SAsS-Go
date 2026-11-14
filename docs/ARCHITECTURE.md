# Architecture

The gateway is a Go service built with hexagonal (ports and adapters)
architecture. Business rules live in `internal/core/domain` and
`internal/core/services`; every external dependency is expressed as a port and
implemented by a swappable adapter.

## Layers

```
cmd/main.go                  process bootstrap, signals, CLI dispatch
internal/app                 dependency graph + HTTP server + worker lifecycle
internal/core/domain
  entities                   aggregates and state machines (payment, escrow, merchant)
  types                      value objects (Amount, chain/network metadata)
  ports                      repository, blockchain, cache, queue, exchange, notification interfaces
internal/core/services       use cases (payments, escrow, webhooks, fraud, exchange, validation)
internal/core/adapters
  primary/http               REST API, middleware, DTOs
  primary/cli                operator commands
  secondary/repositories     in-memory and PostgreSQL persistence
  secondary/blockchain       Ethereum, Bitcoin, Solana, Cardano and mock adapters
  secondary/exchanges        Binance + CryptoCompare with fallback
  secondary/notifications    webhook, email and SMS delivery
  secondary/cache            in-memory and minimal RESP Redis client
  secondary/queue            in-memory queue/pub-sub
internal/workers             expiration, confirmation, address watcher, callbacks, escrow
internal/shared              logger, metrics, tracing, health, crypto/JWT utils
pkg                          public HTTP envelopes and application errors
```

## Payment lifecycle

```
requires_payment ──> processing ──> requires_confirmation ──> succeeded ──> refunded
        │                 │                    │
        ├──> under_review ┘                    ├──> failed
        ├──> canceled                          └──> expired
        ├──> failed
        └──> expired
```

1. A merchant registers and receives an API key (`pk_<env>_<random>`). Only the
   SHA-256 hash and a display prefix are stored.
2. `POST /v1/payments` validates the merchant, resolves the chain/network,
   converts fiat to crypto using the exchange rate (rounded up), applies the
   fraud engine, generates a deposit address, and persists the intent.
3. Incoming funds are detected either by:
   - `POST /v1/webhooks/blockchain` (signed HMAC notification), or
   - `AddressWatcher`, which polls deposit addresses and calls
     `PaymentService.HandleIncoming`.
4. `ConfirmationWorker` refreshes pending transactions against the chain RPC;
   `ExpirationWorker` expires unpaid intents.
5. State changes publish domain events to the queue. `CallbackWorker` fans them
   out to merchant webhooks and email notifications. Failed webhook deliveries
   are retried with exponential backoff by `WebhookRetryWorker`.

## Escrow lifecycle

```
pending ──fund──> funded ──release──> released
                      ├──refund───> refunded
                      └──cancel───> canceled
pending ──expire──> expired
```

Escrows can only be created for succeeded payments. `release_at` is bounded by
`ESCROW_MAX_DURATION`; forced release is disabled unless
`ESCROW_ALLOW_FORCE_RELEASE=true`. `EscrowWorker` releases due escrows.

## Persistence

- `DATABASE_DRIVER=memory` (default): process-local, useful for tests and local
  development.
- `DATABASE_DRIVER=postgres`: schema is created automatically when
  `DATABASE_AUTO_MIGRATE=true` (see
  `internal/core/adapters/secondary/repositories/schema.go`). All monetary
  values are stored as `(value, decimals)` pairs to avoid floating point.
- `CACHE_DRIVER=redis` enables the dependency-free RESP client for exchange
  rate caching.

## Security

- API keys are hashed at rest; JWTs use HS256 with issuer/audience checks.
- Admin endpoints require `X-Admin-Key` compared in constant time.
- Inbound chain webhooks are authenticated with
  `HMAC-SHA256(secret, timestamp + "." + body)` and reject timestamps outside a
  five-minute window.
- Outbound webhooks include `X-Webhook-Timestamp`, `X-Webhook-Id` and
  `X-Signature: sha256=...`.
- Middleware adds security headers, request IDs, body limits, CORS and a token
  bucket rate limiter.
- Secrets are never logged; application errors are mapped to a stable envelope
  and internal causes are only logged server-side.

## Observability

- `/metrics` exposes Prometheus counters and histograms from a dependency-free
  registry (`http_requests_total`, `http_request_duration_seconds`).
- `/healthz` is a liveness probe; `/readyz` checks database, cache, exchange and
  chain RPCs.
- W3C `traceparent` is accepted/propagated when tracing is enabled.
- Structured JSON logs include request IDs, route, status and latency.

## Scaling notes

The HTTP layer is stateless: run multiple replicas behind a load balancer. Use
PostgreSQL and Redis for shared state. The in-memory queue is per-process; for
production fan-out replace `ports.Queue` with Kafka/NATS and keep the same
interface. Hosted chain providers should be used instead of local nodes where
possible.

## Custody note

The adapters verify and broadcast transactions but do not manage private keys.
Ethereum and Solana require `*_DEPOSIT_ADDRESS` to be configured (or a custody
integration) and refunds/escrow releases require a pre-signed raw transaction.
Bitcoin can derive addresses through Bitcoin Core `getnewaddress`. Integrate a
KMS/HSM or custody provider before moving real funds.
