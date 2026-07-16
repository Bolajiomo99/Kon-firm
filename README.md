# Kon-firm

**Omnichannel commerce for Nigerian merchants. One inventory, one ledger — every naira confirmed by Monnify.**

Built for the [API Conference Lagos 2026 Developer Challenge](https://apiconf.net/hackathon), in partnership with [Monnify](https://developers.monnify.com/).

---

## The problem

A merchant in Balogun Market sells across a counter and, increasingly, through a web store. Those are two different systems. Two stock counts that disagree by Friday. Two sets of books, reconciled by hand at closing, from a card terminal receipt roll and a spreadsheet.

The stock discrepancy is the expensive part. Sell the last Adire scarf online at 2pm, and the counter will still happily sell it at 4pm — because the counter has no idea. Someone gets a refund and an apology.

**Kon-firm makes the web store and the shop counter the same system.** One product table, one order table, one payment flow. A sale from either side decrements the same stock and lands in the same ledger, and no sale counts as revenue until Monnify confirms it by signed webhook.

## What it does

| | |
|---|---|
| **Storefront** | Product catalogue, cart, Monnify checkout, receipt that reports what the server actually recorded |
| **POS counter** | Camera barcode scanning (or keyed entry), same inventory, same Monnify flow, tagged `pos` |
| **Admin** | Confirmed revenue, orders split by channel, live inventory with low-stock flags |

## How a payment gets confirmed

This is the core of the project, and the order of operations is deliberate:

1. **The cart is priced server-side.** The browser sends product IDs and quantities. It never sends prices. A client that could set its own price could buy a ₦100,000 item for ₦1.
2. **Monnify initialises the transaction.** We get back a `checkoutUrl`, valid for 40 minutes. We verify the returned `paymentReference` matches what we sent — a mismatch means the response isn't describing our order.
3. **The customer pays on Monnify.** Card details never touch our servers.
4. **A signed webhook settles the order.** HMAC-SHA512 over the raw request body, verified with constant-time comparison, before the body is parsed.

**The browser redirect proves nothing.** It can be replayed or hand-typed, so the callback page never declares success on its own — it polls the server, which reports only what the verified webhook recorded. The webhook is the only path in the system permitted to mark an order paid.

### Replay protection

Monnify's docs are explicit that notifications can repeat, and that you must track what you've processed so you don't give double value. Kon-firm enforces this with a **`UNIQUE (transaction_ref, event_type)` constraint** on the `webhook_events` table, not with a `SELECT` check:

```
check-then-act:   SELECT seen? -> no -> INSERT + credit
                  SELECT seen? -> no -> INSERT + credit    ← both credit
constraint:       INSERT ... ON CONFLICT DO NOTHING
                  -> loser gets 0 rows -> ErrAlreadyProcessed
```

A check-then-act races: two concurrent redeliveries can both observe "unseen" and both credit the order. The database constraint cannot. `TestApplyWebhook_ConcurrentReplays` fires the identical webhook from 8 goroutines and asserts exactly one is applied — it passes against real Postgres, and a check-then-act implementation fails it.

Duplicate webhooks return **200**, so Monnify stops redelivering. Genuine failures return **500**, so it retries.

---

## Quick start

**Prerequisites:** Go 1.24+ and a Postgres database. Nothing else — no Docker, no Node, no local Postgres install.

### 1. Clone

```bash
git clone https://github.com/Bolajiomo99/Kon-firm.git
cd Kon-firm
```

### 2. Get a Postgres database

Any Postgres works. The fastest free option is [Neon](https://neon.tech): create a project and copy the connection string. Using a hosted database from the start means your dev environment matches production.

### 3. Get Monnify sandbox keys

1. Sign in at [app.monnify.com](https://app.monnify.com/).
2. Set the toggle in the top-right to **Test Mode** (it turns orange).
3. Go to **Developer → API Keys & Contracts**.
4. Copy your **API Key** (starts with `MK_TEST_`), **Secret Key**, and **Contract Code**.

You do **not** need to complete business activation or KYC for sandbox access — that's only required to go live with real money.

### 4. Configure

```bash
cp .env.example .env
```

Edit `.env` and fill in:

```ini
MONNIFY_API_KEY=MK_TEST_XXXXXXXXXX
MONNIFY_SECRET_KEY=your_secret_key
MONNIFY_CONTRACT_CODE=your_contract_code
MONNIFY_BASE_URL=https://sandbox.monnify.com
DATABASE_URL=postgresql://user:pass@host.neon.tech/dbname?sslmode=require
KONFIRM_REDIRECT_URL=http://localhost:8080/payment/callback
PORT=8080
```

> `.env` is gitignored. `.env.example` is committed and must only ever hold placeholders.

### 5. Run

```bash
go run ./cmd/server
```

Migrations apply automatically on boot, including a demo catalogue. Open **http://localhost:8080**.

| Page | URL |
|---|---|
| Storefront | http://localhost:8080/ |
| POS counter | http://localhost:8080/pos |
| Admin | http://localhost:8080/admin |
| Health | http://localhost:8080/api/health |

### 6. Make a test payment

1. Add something to the cart and check out — you'll be sent to Monnify's sandbox.
2. Pay with a [Monnify sandbox test card](https://developers.monnify.com/docs/).
3. You'll be returned to the receipt page, which waits for the webhook.

**Webhooks can't reach `localhost`.** Locally, either use a tunnel (`ngrok http 8080`) and set that URL as your webhook in the Monnify dashboard, or exercise the handler directly — see below.

---

## Testing

```bash
go test ./...
```

Database tests are skipped automatically when `DATABASE_URL` is unset, so the suite runs anywhere.

```bash
go test ./internal/monnify/    # signature verification, no database needed
go test ./internal/store/      # idempotency, requires DATABASE_URL
```

Two tests worth knowing about:

- **`TestVerifySignature_MatchesIndependentImplementation`** pins our HMAC against a vector generated in Python's crypto library. If our signature construction ever drifts from the standard one Monnify uses, this fails locally — instead of silently rejecting every webhook in production.
- **`TestApplyWebhook_ConcurrentReplays`** proves the replay protection under concurrency.

### Exercising the webhook by hand

```bash
# Sign a payload exactly as Monnify would.
python3 - <<'PY'
import hmac, hashlib, json
secret = "YOUR_MONNIFY_SECRET_KEY"
body = json.dumps({
  "eventType": "SUCCESSFUL_TRANSACTION",
  "eventData": {
    "transactionReference": "MNFY|TEST|1",
    "paymentReference": "PASTE_AN_ORDER_REFERENCE",
    "paymentStatus": "PAID",
    "amountPaid": 27500.00,
    "totalPayable": 27500.00,
    "currency": "NGN",
    "paymentMethod": "CARD",
    "paidOn": "2026-07-16T12:00:00.000Z",
    "customer": {"name": "Ada", "email": "ada@example.com"}
  }
}, separators=(',', ':'))
print("BODY=" + body)
print("SIG=" + hmac.new(secret.encode(), body.encode(), hashlib.sha512).hexdigest())
PY

curl -X POST http://localhost:8080/api/webhooks/monnify \
  -H 'Content-Type: application/json' \
  -H "monnify-signature: $SIG" \
  -d "$BODY"
```

Send it twice: the first returns `{"status":"processed"}`, the second `{"status":"already processed"}` — and the stock only moves once.

---

## Architecture

```
Kon-firm/
├── cmd/server/         # main: config, migrate, serve, graceful shutdown
├── internal/
│   ├── api/            # HTTP handlers, static serving, security headers
│   ├── config/         # env + .env loading, secret redaction
│   ├── monnify/        # Monnify client: auth, transactions, webhook crypto
│   └── store/          # Postgres: schema, orders, the idempotency ledger
├── frontend/           # embedded into the binary at compile time
└── embed.go            # go:embed root
```

**One binary.** The frontend is embedded, so the API and the pages ship as a single artifact on a single origin. There is no CORS configuration in this project because there is nothing to configure — and the frontend cannot drift to a different version than the API it talks to.

### Design decisions

**Money is `int64` kobo.** Never float. `0.1 + 0.2 != 0.3` in IEEE-754, and a cart accumulated in float drifts. Conversion to decimal naira happens only at the Monnify boundary and at display.

**Prices are read inside the order transaction**, with `FOR UPDATE`. Order items store the price at purchase time, so a later price change never rewrites an old receipt.

**Stock decrements on confirmation, not on checkout.** Filling a cart isn't a sale.

**Secrets are redacted in logs.** `config.Redacted()` masks keys and strips credentials from the database URL, so a log dump can't leak them.

**The barcode scanner uses the browser's native `BarcodeDetector`** with `getUserMedia` — no CDN library, nothing to fail offline. Support is uneven (Chrome and Android yes, Safari no), so manual entry is always available and is a first-class path.

### API

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/health` | Liveness + database ping |
| `GET` | `/api/products` | Catalogue |
| `GET` | `/api/products/barcode/{barcode}` | POS lookup |
| `POST` | `/api/checkout` | Price cart, init Monnify transaction, return `checkoutUrl` |
| `GET` | `/api/orders/{reference}` | Order + receipt |
| `POST` | `/api/webhooks/monnify` | **Signature-verified** settlement |
| `GET` | `/api/admin/overview` | Revenue, recent orders |

---

## Deployment

Deploys as a single Go binary. [`render.yaml`](render.yaml) is included:

1. Push to GitHub.
2. On [Render](https://render.com), **New → Blueprint**, point it at the repo.
3. Set the environment variables from your `.env` in the Render dashboard (never commit them).
4. After the first deploy, set your webhook URL in the Monnify dashboard to:
   `https://<your-app>.onrender.com/api/webhooks/monnify`

`KONFIRM_REDIRECT_URL` must be updated to your deployed URL too.

### Webhook security

- **Signature verification is the gate.** HMAC-SHA512 over the raw body, constant-time compare.
- Monnify's webhook source IP is `35.242.133.146`. Allowlisting it at the edge is reasonable defence in depth, but it is **not** authentication — source IPs are spoofable and egress addresses can change. The signature is what's trusted.

---

## Status

Verified against the Monnify sandbox: authentication, transaction initialisation, a signed webhook settling an order, a forged signature rejected with `401`, and a replayed webhook declining to double-credit.

**Known limitations** — stated plainly, because a submission that hides them is worse than one that doesn't:

- **Admin is unauthenticated.** It's a demo dashboard. Real deployment needs auth in front of it.
- **No refunds or partial payments.** Monnify supports both; Kon-firm records `amountPaid` separately from `totalPayable` so underpayment is visible, but doesn't act on it.
- **Inventory has no reservation window.** Two shoppers can both check out the last item; whoever's webhook lands first gets it, and the second order settles against negative stock.
- **The seeded catalogue is demo data**, not a real product management flow.

## Licence

MIT — see [LICENSE](LICENSE).

---

© 2026 Kon-firm · Built with Monnify Sandbox APIs · Test mode only
