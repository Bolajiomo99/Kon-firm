-- Accounts, sessions, and refunds.

DO $$ BEGIN
    CREATE TYPE user_role AS ENUM ('customer', 'admin');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    -- E.164, e.g. +2348031234567. Normalised on the way in so that the four
    -- ways a Nigerian customer might type their number resolve to one account.
    phone         TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    email         TEXT NOT NULL DEFAULT '',
    -- argon2id, PHC format. Never plaintext, never a fast hash.
    password_hash TEXT NOT NULL,
    role          user_role NOT NULL DEFAULT 'customer',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS users_role_idx ON users (role);

CREATE TABLE IF NOT EXISTS sessions (
    -- SHA-256 of the token, never the token. A leaked backup of this table
    -- yields nothing presentable as a cookie.
    token_hash TEXT PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    user_agent TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS sessions_user_idx ON sessions (user_id);
CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions (expires_at);

-- Orders may now belong to an account. Nullable on purpose: guest checkout
-- stays supported, and orders placed before accounts existed have no owner.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS user_id BIGINT REFERENCES users (id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS orders_user_idx ON orders (user_id, created_at DESC);

-- Refund lifecycle mirrors Monnify's own refundStatus.
DO $$ BEGIN
    CREATE TYPE refund_status AS ENUM ('pending', 'completed', 'failed');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- 'refunded' is a distinct order state: money came in, then went back out.
-- Collapsing it into 'failed' would misreport a sale that genuinely happened
-- and make our books disagree with Monnify's.
--
-- This is deliberately NOT wrapped in a DO block: Postgres refuses
-- ALTER TYPE ... ADD VALUE from inside a function body. IF NOT EXISTS is what
-- makes it re-runnable.
ALTER TYPE order_status ADD VALUE IF NOT EXISTS 'refunded';

CREATE TABLE IF NOT EXISTS refunds (
    id               BIGSERIAL PRIMARY KEY,
    order_id         BIGINT NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    -- Our reference, sent to Monnify. Unique per refund attempt.
    reference        TEXT NOT NULL UNIQUE,
    -- Monnify's transaction reference for the original payment.
    transaction_ref  TEXT NOT NULL,
    -- Monnify's documented minimum refund is ₦100.
    amount_kobo      BIGINT NOT NULL CHECK (amount_kobo >= 10000),
    reason           TEXT NOT NULL,
    status           refund_status NOT NULL DEFAULT 'pending',
    -- Which admin issued it. A refund moves money, so it must be attributable.
    issued_by        BIGINT REFERENCES users (id) ON DELETE SET NULL,
    monnify_comment  TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS refunds_order_idx ON refunds (order_id);
CREATE INDEX IF NOT EXISTS refunds_status_idx ON refunds (status, created_at DESC);
