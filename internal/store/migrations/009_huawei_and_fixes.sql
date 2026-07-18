-- Add Huawei laptop kit, and fix a product whose name did not match its photo.
--
-- Every image below was fetched and looked at before being committed, not
-- guessed from an ID. The rule the shop lives by — the detail must match the
-- picture — has to apply to the shop's own catalogue first.

-- The "Aluminium Desktop Display" photo is an iMac: a whole computer with a
-- screen and keyboard, not a standalone monitor. Sold as a display it is a
-- lie the customer sees the moment the box arrives. Rename it to what the
-- picture actually shows.
UPDATE products
SET name = 'iMac 27-inch (5K)',
    description = 'A 27-inch 5K Retina all-in-one: the display, the computer, and the '
        || 'keyboard in one aluminium body. Wireless keyboard and mouse in the box. '
        || 'Refurbished, graded A, 12-month warranty.',
    category = 'Laptops & Accessories'
WHERE sku = 'KF-LAP-405' OR name = 'Aluminium Desktop Display';

INSERT INTO products
    (sku, barcode, name, description, price_kobo, compare_at_kobo, stock,
     image_url, category, rating, review_count, is_new)
VALUES
    -- A clean silver ultrabook, no competitor logo in frame.
    ('KF-LAP-010', '6009880410016', 'Huawei MateBook 14',
     'A 14-inch 2K touch display in an aluminium body under 1.5kg. Ryzen 7, 16GB '
        || 'RAM, 512GB SSD. All-day battery, fingerprint power button, and a webcam '
        || 'that pops up from the keyboard so nothing is ever watching it.',
     58500000, 67000000, 9,
     'https://images.unsplash.com/photo-1593642702821-c8da6771f0c6?w=800&q=80&fm=jpg&fit=crop',
     'Laptops & Accessories', 4.5, 128, TRUE),

    -- USB-C chargers on a desk — matches a fast charger, not a stand.
    ('KF-LAP-011', '6009880411013', 'Huawei 65W USB-C SuperCharge Adapter',
     'A 65W USB-C fast charger that tops a MateBook to 50% in about 30 minutes, '
        || 'and charges a phone or tablet from the same brick. Foldable pins, '
        || 'braided 1.8m cable in the box.',
     2450000, 3100000, 40,
     'https://images.unsplash.com/photo-1600490722773-35753aea6332?w=800&q=80&fm=jpg&fit=crop',
     'Laptops & Accessories', 4.6, 214, FALSE),

    -- A laptop resting on a felt sleeve.
    ('KF-LAP-012', '6009880412010', 'Huawei 14-inch Laptop Sleeve',
     'A wool-felt sleeve cut for a 14-inch MateBook, with a magnetic flap and a '
        || 'slim front pocket for the charger. Water-repellent, no zips to scratch '
        || 'the lid.',
     1450000, NULL, 55,
     'https://images.unsplash.com/photo-1618424181497-157f25b6ddd5?w=800&q=80&fm=jpg&fit=crop',
     'Laptops & Accessories', 4.4, 96, FALSE)

ON CONFLICT (sku) DO UPDATE SET
    name            = EXCLUDED.name,
    description     = EXCLUDED.description,
    price_kobo      = EXCLUDED.price_kobo,
    compare_at_kobo = EXCLUDED.compare_at_kobo,
    image_url       = EXCLUDED.image_url,
    category        = EXCLUDED.category,
    rating          = EXCLUDED.rating,
    review_count    = EXCLUDED.review_count,
    is_new          = EXCLUDED.is_new,
    active          = TRUE;
