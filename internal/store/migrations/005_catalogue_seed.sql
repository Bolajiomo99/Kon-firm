-- Storefront catalogue.
--
-- Photography is served from Unsplash's CDN under the Unsplash licence, which
-- permits commercial use without attribution. Every URL here was checked to
-- return HTTP 200 with an image content-type before being committed: a guessed
-- photo id is a broken image on a customer's screen, and there is no way to
-- tell the difference from the code.
--
-- The `w` and `q` parameters are deliberate. Unsplash serves originals at
-- several thousand pixels; asking for 800px at q=80 is the difference between
-- a page that loads on Nigerian mobile data and one that does not.
--
-- Prices are kobo. Products are deliberately mid-market Lagos retail, not
-- luxury fantasy: these are prices a real shopper would recognise.

-- Retire the original demo goods; they were placeholder art.
UPDATE products SET active = FALSE
WHERE sku IN ('KF-ANK-001','KF-ADR-002','KF-LTH-003','KF-BDS-004','KF-CER-005','KF-TEX-006');

INSERT INTO products
    (sku, barcode, name, description, price_kobo, compare_at_kobo, stock,
     image_url, category, rating, review_count, is_new)
VALUES
    -- ---------- Fashion ----------
    ('KF-WCH-101', '6009880101017', 'Meridian Automatic Watch',
     'Sapphire crystal over a 41mm brushed steel case, on a Milanese mesh strap. Self-winding movement with a 42-hour reserve — no battery, ever. Water resistant to 50m.',
     18500000, 24900000, 12,
     'https://images.unsplash.com/photo-1523275335684-37898b6baf30?w=800&q=80&fm=jpg&fit=crop',
     'Fashion', 4.8, 214, FALSE),

    ('KF-SNK-102', '6009880102014', 'Court Classic Low Sneaker',
     'Full-grain leather upper on a vulcanised rubber sole. Cushioned insole and a reinforced heel counter that keeps its shape after a Lagos commute.',
     4750000, 6500000, 38,
     'https://images.unsplash.com/photo-1542291026-7eec264c27ff?w=800&q=80&fm=jpg&fit=crop',
     'Fashion', 4.6, 892, TRUE),

    ('KF-BAG-103', '6009880103011', 'Atlas Weekender Bag',
     'Waxed canvas with vegetable-tanned leather trim and solid brass hardware. Fits a 15" laptop, two days of clothes, and a pair of shoes in the base compartment.',
     6200000, NULL, 17,
     'https://images.unsplash.com/photo-1553062407-98eeb64c6a62?w=800&q=80&fm=jpg&fit=crop',
     'Fashion', 4.7, 143, FALSE),

    ('KF-SUN-104', '6009880104018', 'Halide Polarised Sunglasses',
     'Polarised CR-39 lenses in a hand-polished acetate frame. Cuts glare off water and windscreens; 100% UV400. Comes with a hard case and a microfibre cloth.',
     2850000, 3900000, 45,
     'https://images.unsplash.com/photo-1511499767150-a48a237f0083?w=800&q=80&fm=jpg&fit=crop',
     'Fashion', 4.4, 327, FALSE),

    ('KF-SHO-105', '6009880105015', 'Oxford Derby Shoe',
     'Goodyear-welted, so the sole can be replaced rather than the shoe. Full-grain calfskin, leather lining, and a cushioned footbed that breaks in within a week.',
     8900000, NULL, 9,
     'https://images.unsplash.com/photo-1491553895911-0055eca6402d?w=800&q=80&fm=jpg&fit=crop',
     'Fashion', 4.9, 76, FALSE),

    ('KF-EYE-106', '6009880106012', 'Reader Blue-Light Frames',
     'Lightweight acetate with a blue-light filtering coating for long screen days. Spring hinges survive being thrown in a bag. Prescription-ready.',
     1950000, 2600000, 52,
     'https://images.unsplash.com/photo-1572635196237-14b3f281503f?w=800&q=80&fm=jpg&fit=crop',
     'Fashion', 4.3, 418, TRUE),

    -- ---------- Gadgets ----------
    ('KF-HPH-201', '6009880201014', 'Studio Wireless Headphones',
     'Active noise cancellation with 40mm drivers and 30 hours of playback. Multipoint pairing holds your phone and laptop at once. Folds flat into the included case.',
     12500000, 16900000, 23,
     'https://images.unsplash.com/photo-1505740420928-5e560c06d30e?w=800&q=80&fm=jpg&fit=crop',
     'Gadgets', 4.7, 1204, FALSE),

    ('KF-SWT-202', '6009880202011', 'Pulse Fitness Smartwatch',
     'Continuous heart rate, blood oxygen, and sleep tracking on a 1.4" AMOLED display. Seven-day battery, 5ATM water resistance, and built-in GPS that does not need your phone.',
     9800000, 13500000, 31,
     'https://images.unsplash.com/photo-1546868871-7041f2a55e12?w=800&q=80&fm=jpg&fit=crop',
     'Gadgets', 4.5, 668, TRUE),

    ('KF-KBD-203', '6009880203018', 'Mechanical Keyboard 87',
     'Tenkeyless layout with hot-swappable tactile switches and double-shot PBT keycaps. USB-C, detachable braided cable, and an aluminium plate that kills the ping.',
     7400000, NULL, 19,
     'https://images.unsplash.com/photo-1434493789847-2f02dc6ca35d?w=800&q=80&fm=jpg&fit=crop',
     'Gadgets', 4.8, 352, FALSE),

    ('KF-CAM-204', '6009880204015', 'Rangefinder Mirrorless Camera',
     '24MP APS-C sensor with in-body stabilisation and a hybrid viewfinder. Shoots 4K/60. The dials are real dials — shutter speed and ISO without a menu.',
     42500000, 49900000, 4,
     'https://images.unsplash.com/photo-1526170375885-4d8ecf77b99f?w=800&q=80&fm=jpg&fit=crop',
     'Gadgets', 4.9, 89, FALSE),

    -- ---------- Accessories ----------
    ('KF-ACC-301', '6009880301011', 'Everyday Leather Backpack',
     'Vegetable-tanned leather that darkens with use. Padded 16" laptop sleeve, a hidden back pocket for a passport, and YKK zips throughout.',
     11200000, 14500000, 14,
     'https://images.unsplash.com/photo-1560343090-f0409e92791a?w=800&q=80&fm=jpg&fit=crop',
     'Accessories', 4.6, 231, FALSE),

    ('KF-ACC-302', '6009880302018', 'Aviator Metal Sunglasses',
     'Thin stainless frame with gradient lenses and silicone nose pads that do not slip in the heat. Weighs 22 grams — you forget you have them on.',
     3400000, NULL, 27,
     'https://images.unsplash.com/photo-1523206489230-c012c64b2b48?w=800&q=80&fm=jpg&fit=crop',
     'Accessories', 4.2, 156, FALSE)

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
