-- Kon-firm initial schema.
--
-- Money is stored in kobo (integer minor units), never as float/double.
-- Floating point cannot represent 0.10 exactly, so accumulating a cart in
-- float silently drifts. All arithmetic happens in int64 kobo; conversion to
-- decimal naira occurs only at the Monnify API boundary.

CREATE TABLE IF NOT EXISTS products (
    id          BIGSERIAL PRIMARY KEY,
    sku         TEXT NOT NULL UNIQUE,
    barcode     TEXT UNIQUE,               -- scanned in POS mode; NULL for online-only items
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    price_kobo  BIGINT NOT NULL CHECK (price_kobo > 0),
    stock       INTEGER NOT NULL DEFAULT 0 CHECK (stock >= 0),
    image_url   TEXT NOT NULL DEFAULT '',
    active      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS products_barcode_idx ON products (barcode) WHERE barcode IS NOT NULL;

-- Order lifecycle: pending -> paid | failed | expired.
-- Only a signature-verified Monnify webhook may move an order to 'paid'.
-- Nothing the browser sends can transition an order; the client is not trusted.
--
-- CREATE TYPE has no IF NOT EXISTS, so guard it to keep this file re-runnable.
DO $$ BEGIN
    CREATE TYPE order_status AS ENUM ('pending', 'paid', 'failed', 'expired');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- How the sale originated. This is the "omnichannel" claim, made concrete.
DO $$ BEGIN
    CREATE TYPE order_channel AS ENUM ('online', 'pos');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS orders (
    id              BIGSERIAL PRIMARY KEY,
    -- Our reference, sent to Monnify as paymentReference. Must be globally
    -- unique: Monnify rejects a reused paymentReference.
    reference       TEXT NOT NULL UNIQUE,
    -- Monnify's own reference, known only after init-transaction returns.
    transaction_ref TEXT UNIQUE,
    customer_name   TEXT NOT NULL,
    customer_email  TEXT NOT NULL,
    total_kobo      BIGINT NOT NULL CHECK (total_kobo > 0),
    status          order_status NOT NULL DEFAULT 'pending',
    channel         order_channel NOT NULL DEFAULT 'online',
    checkout_url    TEXT NOT NULL DEFAULT '',
    -- What Monnify says was actually paid. May differ from total_kobo on
    -- underpayment, so we record both rather than assuming they match.
    amount_paid_kobo BIGINT,
    payment_method  TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    paid_at         TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS orders_status_idx ON orders (status, created_at DESC);
CREATE INDEX IF NOT EXISTS orders_email_idx ON orders (customer_email, created_at DESC);

CREATE TABLE IF NOT EXISTS order_items (
    id              BIGSERIAL PRIMARY KEY,
    order_id        BIGINT NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    product_id      BIGINT NOT NULL REFERENCES products (id),
    quantity        INTEGER NOT NULL CHECK (quantity > 0),
    -- Price captured at purchase time. Never join to products for historical
    -- pricing: a later price change must not rewrite past receipts.
    unit_price_kobo BIGINT NOT NULL CHECK (unit_price_kobo > 0),
    product_name    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS order_items_order_idx ON order_items (order_id);

-- The idempotency ledger.
--
-- Monnify's docs warn explicitly that notifications can repeat, and that you
-- must track what you've already processed so you don't give double value.
-- The UNIQUE constraint on transaction_reference is what enforces that: a
-- replayed webhook loses the INSERT race and is acknowledged without being
-- applied a second time. This is a database-level guarantee, not application
-- logic that a concurrent request could slip past.
CREATE TABLE IF NOT EXISTS webhook_events (
    id                BIGSERIAL PRIMARY KEY,
    transaction_ref   TEXT NOT NULL,
    event_type        TEXT NOT NULL,
    payload           JSONB NOT NULL,
    received_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- One row per (transaction, event type). A SUCCESSFUL_TRANSACTION and a
    -- later SETTLEMENT for the same transaction are distinct events, but the
    -- same event delivered twice collapses to one row.
    UNIQUE (transaction_ref, event_type)
);

CREATE INDEX IF NOT EXISTS webhook_events_received_idx ON webhook_events (received_at DESC);
