-- Catalogue fields a real storefront needs.

ALTER TABLE products ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT 'Other';

-- The "was" price. Nullable: most things are not on sale, and a NULL says so
-- honestly. Storing the current price here when nothing is discounted would
-- render a struck-through price identical to the real one.
ALTER TABLE products ADD COLUMN IF NOT EXISTS compare_at_kobo BIGINT
    CHECK (compare_at_kobo IS NULL OR compare_at_kobo > 0);

ALTER TABLE products ADD COLUMN IF NOT EXISTS rating NUMERIC(2,1)
    CHECK (rating IS NULL OR (rating >= 0 AND rating <= 5));
ALTER TABLE products ADD COLUMN IF NOT EXISTS review_count INTEGER NOT NULL DEFAULT 0
    CHECK (review_count >= 0);

-- Marks a product as new for the badge, independent of created_at, so the
-- shop can decide what "new" means rather than the clock deciding for it.
ALTER TABLE products ADD COLUMN IF NOT EXISTS is_new BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS products_category_idx ON products (category) WHERE active;

-- A discount must actually be a discount. Without this, a careless import
-- could show "was ₦20,000, now ₦25,000" — which is worse than showing nothing.
DO $$ BEGIN
    ALTER TABLE products ADD CONSTRAINT products_compare_at_is_higher
        CHECK (compare_at_kobo IS NULL OR compare_at_kobo > price_kobo);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
