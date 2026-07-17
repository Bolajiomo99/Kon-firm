-- Delivery, tax, and vouchers.
--
-- Money stays int64 kobo. Every figure below is derived server-side at
-- checkout and frozen onto the order: a voucher that expires, a VAT rate that
-- changes, or a delivery fee that is revised must never rewrite what a
-- customer was actually charged last month.

ALTER TABLE orders ADD COLUMN IF NOT EXISTS delivery_name    TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN IF NOT EXISTS delivery_phone   TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN IF NOT EXISTS delivery_address TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN IF NOT EXISTS delivery_city    TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN IF NOT EXISTS delivery_state   TEXT NOT NULL DEFAULT '';
-- Captured when the shopper uses "use my location". Nullable: most will type
-- an address, and a coordinate is a convenience for the dispatch rider, never
-- a substitute for the written address.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS delivery_lat NUMERIC(9,6);
ALTER TABLE orders ADD COLUMN IF NOT EXISTS delivery_lng NUMERIC(9,6);

-- The money breakdown, all frozen at purchase time.
-- subtotal is VAT-inclusive, matching the shelf price.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS subtotal_kobo     BIGINT NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS delivery_fee_kobo BIGINT NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS discount_kobo     BIGINT NOT NULL DEFAULT 0;
-- The VAT contained within the total, not added on top.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS vat_kobo          BIGINT NOT NULL DEFAULT 0;
-- The rate is stored per order. Nigeria has held 7.5% since 2020 and the
-- Nigeria Tax Act 2025 kept it, but a rate is a policy, not a constant — an
-- old receipt must still add up after the next budget.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS vat_rate_bp INTEGER NOT NULL DEFAULT 750; -- basis points
ALTER TABLE orders ADD COLUMN IF NOT EXISTS voucher_code TEXT NOT NULL DEFAULT '';

DO $$ BEGIN
    CREATE TYPE discount_kind AS ENUM ('percent', 'fixed');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS vouchers (
    code           TEXT PRIMARY KEY,
    kind           discount_kind NOT NULL,
    -- percent: basis points (1000 = 10%). fixed: kobo off.
    value          BIGINT NOT NULL CHECK (value > 0),
    -- Guards against a 10% code quietly discounting a ₦4m order by ₦400k.
    max_discount_kobo BIGINT CHECK (max_discount_kobo IS NULL OR max_discount_kobo > 0),
    min_spend_kobo BIGINT NOT NULL DEFAULT 0 CHECK (min_spend_kobo >= 0),
    -- NULL means unlimited.
    max_uses       INTEGER CHECK (max_uses IS NULL OR max_uses > 0),
    times_used     INTEGER NOT NULL DEFAULT 0 CHECK (times_used >= 0),
    expires_at     TIMESTAMPTZ,
    active         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO vouchers (code, kind, value, max_discount_kobo, min_spend_kobo, max_uses, expires_at) VALUES
    ('WELCOME10',  'percent', 1000, 1500000, 2000000, NULL, NULL),
    ('LAGOS5000',  'fixed',   500000, NULL,  5000000, 200,  NULL),
    ('APICONF26',  'percent', 1500, 2500000, 3000000, NULL, '2026-08-01 00:00:00+01')
ON CONFLICT (code) DO NOTHING;
