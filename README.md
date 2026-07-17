<div align="center">

<img src="frontend/img/logo-512.png" alt="Kon-firm" width="120">

# Kon-firm

**Omnichannel commerce for Nigerian merchants. One inventory, one ledger — every naira confirmed by Monnify.**

[**Live app →**](https://konfirm.onrender.com) · Built for the [API Conference Lagos 2026 Developer Challenge](https://apiconf.net/hackathon) with [Monnify](https://developers.monnify.com/)

</div>

---

## The story: our webhook failed, and the customer still got paid up

This is a real production log from this project:

```
01:21:55  POST /api/webhooks/monnify   status=400
01:23:49  POST /api/webhooks/monnify   status=400
```

Monnify took a real ₦45,000 payment. It tried to tell us. **Twice.** Both times our server rejected the notification with a 400, because `paidOn` arrives as `"17/11/2021 3:48:10 PM"` and Go's `encoding/json` only unmarshals RFC 3339 into a `time.Time`. Every hand-written test fixture passed, because fixtures get written in RFC 3339 out of habit — the tests encoded the same wrong assumption as the code.

Meanwhile the customer had paid, and the order sat at `pending`.

**Most integrations would have stopped there** — a customer out of pocket, an order stuck, and a merchant refreshing a dashboard that will never change.

Kon-firm asked Monnify directly instead:

```
GET /api/v2/transactions/MNFY|41|20260717031412|000179  →  paymentStatus: PAID
```

The order settled. The customer was confirmed. **The push failed and the system was still correct**, because a webhook is a fast path, not a source of truth — which is exactly what [Monnify's own documentation says](https://developers.monnify.com/docs/collections/manage-payments/verify-transactions):

> Always verify a transaction on your server using the Monnify API before delivering goods, granting access, or crediting a wallet.

That principle — *never trust a push you can verify* — is the spine of this project.

---

## The problem

A merchant in Balogun Market sells across a counter and, increasingly, online. Those are two different systems: two stock counts that disagree by Friday, and two sets of books reconciled by hand at closing.

The stock discrepancy is the expensive part. Sell the last scarf online at 2pm and the counter will still sell it at 4pm — because the counter has no idea. Someone gets a refund and an apology.

**Kon-firm makes the web store and the shop counter the same system.** One product table, one order table, one payment flow. A sale from either side decrements the same stock, and nothing counts as revenue until Monnify confirms it.

## What it does

| | |
|---|---|
| **Storefront** | Catalogue, bag, delivery, vouchers, VAT-inclusive totals, Monnify checkout |
| **POS counter** | Camera barcode scanning, same inventory, same payment flow, tagged `pos` |
| **Admin** | Live revenue, orders by channel, refunds through Monnify, full product management |
| **Accounts** | WhatsApp number as identity, argon2id passwords, order history — guest checkout still works |
| **Live updates** | Server-Sent Events: a confirmed payment updates every open screen with no refresh |

---

## Monnify API surface used

This is a collections integration taken further than the happy path.

| Endpoint | What we use it for |
|---|---|
| `POST /api/v1/auth/login` | Bearer token, cached with early refresh |
| `POST /api/v1/merchant/transactions/init-transaction` | Start a payment, get the checkout URL |
| `GET /api/v2/transactions/{ref}` | **Verify server-side — the fallback that saved the order above** |
| `GET /api/v1/merchant/transactions/query` | Verify by our own payment reference |
| `POST /api/v1/refunds/initiate-refund` | Refund from the admin dashboard |
| Webhooks | `SUCCESSFUL_TRANSACTION`, `FAILED_TRANSACTION`, `SUCCESSFUL_REFUND`, `FAILED_REFUND` |

### How a payment gets confirmed

```
1. Bag is priced SERVER-SIDE      the browser sends product IDs, never prices
2. Monnify initialises            we get a checkoutUrl, valid 40 minutes
3. Customer pays on Monnify       card details never touch our servers
4. Signed webhook settles         HMAC-SHA512 over the RAW body, constant-time compare
   └─ if that fails ─────────────▶ 4b. We ask Monnify directly and settle anyway
```

**The browser redirect proves nothing.** It can be replayed or hand-typed, so the receipt page never declares success on its own — it reports only what the server recorded. The webhook and the verification API are the only two things that can mark an order paid.

### Replay protection

Monnify's docs warn that notifications repeat. Kon-firm enforces that with a **`UNIQUE (transaction_ref, event_type)` constraint**, not a `SELECT`:

```
check-then-act:   SELECT seen? → no → INSERT + credit
                  SELECT seen? → no → INSERT + credit    ← both credit. Bug.

constraint:       INSERT ... ON CONFLICT DO NOTHING
                  → loser gets 0 rows → ErrAlreadyProcessed → no double credit
```

`TestApplyWebhook_ConcurrentReplays` fires the identical webhook from 8 goroutines and asserts exactly one is applied. It passes against real Postgres. A check-then-act implementation passes the sequential test and **fails this one**.

Duplicate webhooks return **200** so Monnify stops redelivering. Genuine failures return **500** so it retries.

---

## Quick start

**Prerequisites:** Go 1.25+ (the floor `pgx` requires) and any Postgres. No Docker, no Node, no local database install.

### 1. Clone

```bash
git clone https://github.com/Bolajiomo99/Kon-firm.git
cd Kon-firm
```

### 2. Get a Postgres

Any Postgres works. Fastest free option is [Neon](https://neon.tech): create a project, copy the connection string. Using a hosted database from the start means dev matches production.

### 3. Get Monnify sandbox keys

1. Sign in at [app.monnify.com](https://app.monnify.com/)
2. Set the toggle top-right to **Test Mode** (it turns orange)
3. **Developer → API Keys & Contracts**
4. Copy the **API Key** (`MK_TEST_…`), **Secret Key**, and **Contract Code**

> You do **not** need business activation or KYC for sandbox — that only gates going live.

### 4. Configure

```bash
cp .env.example .env
```

```ini
MONNIFY_API_KEY=MK_TEST_XXXXXXXXXX
MONNIFY_SECRET_KEY=your_secret_key
MONNIFY_CONTRACT_CODE=your_contract_code
MONNIFY_BASE_URL=https://sandbox.monnify.com
DATABASE_URL=postgresql://user:pass@host.neon.tech/dbname?sslmode=require
KONFIRM_REDIRECT_URL=http://localhost:8080/payment/callback

# The first admin, created on boot — nobody can grant a role before one exists.
KONFIRM_ADMIN_PHONE=08031234567
KONFIRM_ADMIN_PASSWORD=change-me-2026
KONFIRM_ADMIN_NAME=Store Manager

PORT=8080
ENV=development
```

> `.env` is gitignored. `.env.example` is committed and must only ever hold placeholders.

### 5. Run

```bash
go run ./cmd/server
```

Migrations apply on boot, including a 20-product demo catalogue.

| Page | URL | Access |
|---|---|---|
| Storefront | http://localhost:8080/ | Public |
| Sign in / up | http://localhost:8080/login | Public |
| My orders | http://localhost:8080/orders | Signed in |
| Admin | http://localhost:8080/admin | Admin only |
| POS counter | http://localhost:8080/pos | Admin only |

Sign in with the `KONFIRM_ADMIN_PHONE` / `KONFIRM_ADMIN_PASSWORD` you set. Any phone format works — `0803…`, `+234803…`, `803…` all resolve to one account.

### 6. Make a test payment

1. Add something to the bag, fill in a Lagos address, try voucher **`WELCOME10`**
2. Check out — you'll land on Monnify's sandbox
3. Choose **Transfer**, then open the [Monnify Bank Simulator](https://websim.sdk.monnify.com/?#/bankingapp)
4. Enter the displayed account number and the **exact** amount

> **Webhooks cannot reach `localhost`.** Locally, either tunnel (`ngrok http 8080`) and set that URL in the Monnify dashboard, or exercise the handler directly — see below. **Even with no webhook at all, the receipt page still confirms**, because it falls back to verification.

---

## Testing

```bash
go test ./...        # database tests skip automatically without DATABASE_URL
```

**30 test functions.** The ones that matter:

- **`TestVerifySignature_MatchesIndependentImplementation`** — pins our HMAC against a vector generated in Python's crypto library. If our signature construction drifts from the standard, this fails locally instead of silently rejecting every webhook in production.
- **`TestParseWebhook_RealMonnifyPayload`** — Monnify's documented payload verbatim, `"17/11/2021 3:48:10 PM"` timestamp included. This is the regression test for the 400s above.
- **`TestTransaction_DecodesRealSandboxResponse`** — a **captured sandbox response**, not a hand-written fixture. Every bug in that package came from a fixture someone typed: the docs and reality disagree about field names and about whether money is a number or a string.
- **`TestApplyWebhook_ConcurrentReplays`** — 8 goroutines, one credit.
- **`TestVATWithin_IsInclusiveNotAdditive`** — guards against a later "fix" inverting the tax and inflating every total by 7.5%.
- **`TestCreateOrder_RefusesWhenPriceMoved`** — a stale quote must not be honoured.

### Exercising the webhook by hand

```bash
python3 - <<'PY'
import hmac, hashlib, json
secret = "YOUR_MONNIFY_SECRET_KEY"
body = json.dumps({
  "eventType": "SUCCESSFUL_TRANSACTION",
  "eventData": {
    "transactionReference": "MNFY|TEST|1",
    "paymentReference": "PASTE_AN_ORDER_REFERENCE",
    "paymentStatus": "PAID",
    "amountPaid": "27500.00",
    "totalPayable": "27500.00",
    "paidOn": "17/07/2026 4:30:00 AM",
    "currency": "NGN",
    "paymentMethod": "ACCOUNT_TRANSFER",
    "customer": {"name": "Ada", "email": "ada@example.com"}
  }
}, separators=(',', ':'))
print("BODY=" + body)
print("SIG=" + hmac.new(secret.encode(), body.encode(), hashlib.sha512).hexdigest())
PY

curl -X POST http://localhost:8080/api/webhooks/monnify \
  -H 'Content-Type: application/json' \
  -H "monnify-signature: $SIG" -d "$BODY"
```

Send it **twice**: the first returns `{"status":"processed"}`, the second `{"status":"already processed"}` — and the stock only moves once. Send it with a wrong signature and it returns **401**.

---

## Architecture

```
Kon-firm/
├── cmd/server/          main: config, migrate, serve, graceful shutdown
├── internal/
│   ├── api/             HTTP handlers, SSE stream, security headers, static serving
│   ├── auth/            argon2id passwords, sessions, phone normalisation
│   ├── config/          env + .env loading, secret redaction
│   ├── events/          in-process pub/sub for live updates
│   ├── monnify/         auth, transactions, verification, refunds, webhook crypto
│   └── store/           Postgres: schema, orders, pricing, the idempotency ledger
├── frontend/            embedded into the binary at compile time
└── embed.go             go:embed root
```

**One binary.** The frontend is embedded, so the API and the pages ship as a single artifact on one origin. There is no CORS configuration in this project because there is nothing to configure — and the frontend cannot drift to a different version than the API it talks to.

**Two direct dependencies:** `pgx` (Postgres) and `golang.org/x/crypto` (argon2) — everything else in `go.mod` is `pgx`'s own. No web framework, no ORM, no frontend build step, no `node_modules`.

### Decisions worth defending

**Money is `int64` kobo. Never float.** `0.1 + 0.2 != 0.3` in IEEE-754. Conversion to decimal naira happens only at the Monnify boundary and at display.

**VAT is extracted, not added.** Nigeria's rate is **7.5%** — the [Nigeria Tax Act 2025](https://www.ey.com/en_gl/technical/tax-alerts/nigeria-tax-act-2025-has-been-signed-highlights), effective 1 January 2026, kept it there despite proposals to raise it. Shelf prices *include* VAT, as Nigerian retail expects, so the price on the product page is the price Monnify charges. Adding 7.5% at the payment step would change the number the customer already agreed to at the exact moment they're asked to pay it. The rate is stored **per order** in basis points: 7.5 isn't representable in binary floating point, and a rate is policy, not a constant — an old receipt must still add up after the next budget.

**Prices are read inside the order transaction**, with `FOR UPDATE`, and re-checked against the quote. If a price moved mid-checkout the order is **refused**: charging the new price bills for something never agreed to; charging the old one sells at a withdrawn price.

**Stock decrements on confirmation, not on checkout.** Filling a bag isn't a sale.

**A product with orders against it is hidden, never deleted.** A hard delete cascades `order_items` away and quietly rewrites history — past receipts lose their lines and the books stop adding up.

**Live updates are best-effort by design.** Events nudge a page to re-read; they are never the data. Everything is committed to Postgres *before* an event is published, so a dropped frame costs seconds of staleness, not correctness.

**Secrets are redacted in logs.** `config.Redacted()` masks keys and strips credentials from the database URL.

### API

| Method | Path | Access |
|---|---|---|
| `GET` | `/api/health` | Public |
| `GET` | `/api/products` | Public |
| `POST` | `/api/quote` | Public — server-computed totals |
| `POST` | `/api/checkout` | Public — guest or signed in |
| `GET` | `/api/orders/{reference}` | Public — falls back to verification |
| `POST` | `/api/auth/signup` · `/login` · `/logout` | Public |
| `GET` | `/api/auth/me` · `/api/me/orders` | Session |
| `GET` | `/api/stream` | SSE — scope from session |
| `POST` | `/api/webhooks/monnify` | **Signature-verified** |
| `GET` | `/api/products/barcode/{barcode}` | Admin |
| `GET` | `/api/admin/overview` | Admin |
| `POST` | `/api/admin/orders/{reference}/refund` | Admin |
| `GET`/`POST`/`PUT`/`DELETE` | `/api/admin/products` | Admin |

Non-admins get **404**, not 403, from admin routes: a 403 confirms the route exists and maps the admin surface for anyone probing.

---

## Security

- **Webhook signature is the gate.** HMAC-SHA512 over the **raw request bytes** — re-marshalling JSON changes key order and whitespace, which changes the hash and rejects legitimate requests. Constant-time comparison.
- **Monnify's webhook IP is `35.242.133.146`.** Allowlisting it at the edge is reasonable defence in depth, but it is **not** authentication — source IPs are spoofable.
- **Passwords are argon2id** at OWASP's parameters, PHC-encoded so the cost can be raised without invalidating existing hashes.
- **Session tokens are 256 bits from `crypto/rand`; only their SHA-256 is stored.** A leaked sessions table yields nothing presentable as a cookie.
- **Login answers identically** for a wrong password and an unknown account, and hashes a dummy on the missing path so timing can't enumerate the customer list.
- **Signup never reads a role from the request body.** A self-service admin would be an admin for the asking.
- Cookies are `__Host-` prefixed, `HttpOnly`, `Secure`, `SameSite=Lax`.
- **CSP is tight**: `script-src 'self'`, `connect-src 'self'`. `img-src` names `images.unsplash.com` and nothing else does — the image host can neither run code nor receive data.

---

## Deployment

Single Go binary. [`render.yaml`](render.yaml) is included:

1. Push to GitHub
2. [Render](https://render.com) → **New → Blueprint** → point at the repo
3. Render prompts for the secrets (`sync: false` keeps them out of the file)
4. After the first deploy, set:
   - `KONFIRM_REDIRECT_URL` → `https://<your-app>.onrender.com/payment/callback`
   - **Monnify dashboard → Developer → Webhook URLs → Transaction Completion** → `https://<your-app>.onrender.com/api/webhooks/monnify`

> The dashboard toggle must be on **Test Mode** when you save the webhook. Webhook URLs are per-environment; saving it in Live mode means sandbox payments never call you.

**Free tier sleeps after ~15 min idle** — a cold start takes ~30–50s. Hit the URL a minute before a demo. It doesn't affect webhooks (Monnify retries), but a sleeping app looks broken when it isn't.

---

## Known limitations

Stated plainly, because a submission that hides them is worse than one that doesn't.

- **No inventory reservation window.** Two shoppers can both check out the last item; whoever's payment confirms first gets it, and the second settles against negative stock.
- **Partial refunds don't restore stock.** Only a full refund does — a partial refund is usually a price adjustment, not a return, but the code can't tell the difference.
- **Settlement status isn't tracked.** Monnify settles to the merchant on its own schedule; Kon-firm records that a payment was *confirmed*, not that it was *paid out*.
- **Live updates are in-process.** With more than one instance, a client connected to instance A misses events published on B. The page stays correct — it just updates on next fetch instead of instantly. Redis would fix it; one instance doesn't need it.
- **Product photography is served from Unsplash** under its licence. Every URL is verified to return 200, and each image degrades to a labelled placeholder if the CDN is unreachable — but it is a third-party dependency.
- **No AI.** Deliberate. The rules encourage it and warn that "AI slop is greatly frowned upon"; a bolted-on assistant would have been slop.
- **Sandbox only**, per the challenge rules.

## Licence

MIT — see [LICENSE](LICENSE).

---

<div align="center">

**© 2026 Kon-firm** · Built with Monnify Sandbox APIs · Test mode only

</div>
