# Webhooks

## Outbound merchant webhooks

Events are published asynchronously after state changes. Delivery attempts are
persisted in `webhook_deliveries` and retried with exponential backoff (5s, 10s,
20s, ... capped at 1h) up to `WEBHOOK_MAX_RETRIES` attempts.

Headers:

| Header | Value |
| --- | --- |
| `Content-Type` | `application/json` |
| `User-Agent` | `WEBHOOK_USER_AGENT` |
| `X-Webhook-Timestamp` | Unix seconds |
| `X-Webhook-Id` | Delivery UUID |
| `X-Event-Type` | Event type |
| `X-Signature` | `sha256=<hex HMAC-SHA256(secret, timestamp + "." + body)>` |

Payload:

```json
{
  "id": "3f2a...",
  "type": "payment_intent.succeeded",
  "aggregate_type": "payment_intent",
  "aggregate_id": "e1fb...",
  "merchant_id": "e166...",
  "created_at": "2026-01-01T12:00:00Z",
  "data": {
    "payment_intent_id": "e1fb...",
    "status": "succeeded",
    "amount": "25.00",
    "currency": "USD",
    "crypto_amount": "0.01",
    "crypto_currency": "ETH",
    "blockchain": "ethereum",
    "network": "ethereum_sepolia",
    "deposit_address": "0x...",
    "tx_hash": "0x...",
    "expires_at": "2026-01-01T12:15:00Z"
  }
}
```

### Event types

- `payment_intent.created`
- `payment_intent.updated`
- `payment_intent.succeeded`
- `payment_intent.failed`
- `payment_intent.expired`
- `payment_intent.canceled`
- `payment_intent.refunded`
- `escrow.created`
- `escrow.released`
- `escrow.refunded`
- `escrow.canceled`
- `webhook.test`

### Verifying a signature

```python
import hmac, hashlib

def verify(secret: str, timestamp: str, body: bytes, signature: str) -> bool:
    expected = hmac.new(secret.encode(), f"{timestamp}.".encode() + body, hashlib.sha256).hexdigest()
    received = signature.removeprefix("sha256=")
    return hmac.compare_digest(expected, received)
```

Reject timestamps older than a few minutes to prevent replay, and respond with a
2xx status quickly. Any non-2xx response is retried.

`POST /v1/webhooks/test` sends a test event to the merchant webhook URL.

## Inbound blockchain notifications

External chain listeners (or hosted providers) can push transactions to
`POST /v1/webhooks/blockchain`. When `INBOUND_WEBHOOK_SECRET` is set, the request
must include `X-Webhook-Timestamp` and `X-Signature` using the same HMAC scheme.
Timestamps outside a five-minute window are rejected to prevent replay.

```bash
BODY='{"blockchain":"ethereum","network":"sepolia","tx_hash":"0xabc","to":"0xdeposit","amount":"0.01","confirmations":12,"status":"confirmed"}'
TS=$(date +%s)
SIG=$(printf '%s.%s' "$TS" "$BODY" | openssl dgst -sha256 -hmac "$INBOUND_WEBHOOK_SECRET" -hex | awk '{print $2}')
curl -X POST http://localhost:8080/v1/webhooks/blockchain \
  -H "Content-Type: application/json" \
  -H "X-Webhook-Timestamp: $TS" \
  -H "X-Signature: sha256=$SIG" \
  -d "$BODY"
```

Responses:

- `200` — a matching payment intent was found and updated.
- `202` — accepted but no matching intent (or duplicate transaction hash).
- `401` — invalid or missing signature.
