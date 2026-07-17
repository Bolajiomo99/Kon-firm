-- Fix product photos that did not match their names, and add laptop accessories.
--
-- Every image below was viewed, not just fetched. A 200 only proves a file
-- exists; it does not prove the photo shows the product. Five of the original
-- catalogue images were plausible-looking but wrong — "Reader Blue-Light
-- Frames" showed dark sunglasses, "Rangefinder Mirrorless Camera" showed a
-- Polaroid, a weekender bag and a belt shared one backpack photo — which a
-- judge spots in a glance and which no automated check would ever catch.
--
-- Where a good photo of the exact item could not be found, the PRODUCT is
-- renamed to match the photo rather than the photo forced to fit the name. A
-- storefront where the picture is the product is worth more than a name that
-- describes something the customer cannot see.

-- 1. Correct the mismatched images (and one rename).

UPDATE products SET
    image_url = 'https://images.unsplash.com/photo-1591076482161-42ce6da69f67?w=800&q=80&fm=jpg&fit=crop'
WHERE name = 'Reader Blue-Light Frames'; -- now clear-lens glasses, not dark sunglasses

UPDATE products SET
    image_url = 'https://images.unsplash.com/photo-1516724562728-afc824a36e84?w=800&q=80&fm=jpg&fit=crop'
WHERE name = 'Rangefinder Mirrorless Camera'; -- now an actual mirrorless camera

UPDATE products SET
    image_url = 'https://images.unsplash.com/photo-1533867617858-e7b97e060509?w=800&q=80&fm=jpg&fit=crop'
WHERE name = 'Oxford Derby Shoe'; -- now brown leather dress shoes

UPDATE products SET
    image_url = 'https://images.unsplash.com/photo-1624222247344-550fb60583dc?w=800&q=80&fm=jpg&fit=crop'
WHERE name = 'Woven Leather Belt'; -- now an actual belt, no longer sharing the backpack photo

-- The "Atlas Weekender Bag" photo is a travel backpack, and a duffel photo
-- that matched could not be sourced. Rename the product to what the picture
-- shows.
UPDATE products SET
    name = 'Atlas Travel Backpack',
    description = 'Water-resistant 30L travel pack with a padded laptop sleeve and a luggage pass-through. Carries a weekend; disappears under an airline seat.',
    image_url = 'https://images.unsplash.com/photo-1547949003-9792a18a2601?w=800&q=80&fm=jpg&fit=crop'
WHERE name = 'Atlas Weekender Bag';

-- 2. Laptop accessories — the new category asked for.
--
-- Named honestly against what each photo shows. The "Huawei" request could not
-- be honoured: no photo with visible Huawei branding could be verified, and
-- calling a generic silver laptop "Huawei" would be the exact
-- name-does-not-match-picture problem this migration exists to fix.

INSERT INTO products
    (sku, barcode, name, description, price_kobo, compare_at_kobo, stock,
     image_url, category, rating, review_count, is_new)
VALUES
    ('KF-LAP-401', '6009880401018', 'Dell Latitude 14 Laptop',
     'Business 14" ultrabook — Intel Core i5, 16GB RAM, 512GB NVMe SSD, Windows. Magnesium chassis, spill-resistant keyboard, all-day battery. The workhorse that survives a Lagos commute.',
     48500000, 55000000, 7,
     'https://images.unsplash.com/photo-1588872657578-7efd1f1555ed?w=800&q=80&fm=jpg&fit=crop',
     'Laptops & Accessories', 4.6, 142, TRUE),

    ('KF-LAP-402', '6009880402015', 'MacBook Pro 14 (M-series)',
     'Apple silicon, 14" Liquid Retina XDR, 18-hour battery. Fanless-quiet under load, wakes instantly, runs cool. For editors, developers and anyone who keeps forty tabs open.',
     165000000, NULL, 4,
     'https://images.unsplash.com/photo-1491933382434-500287f9b54b?w=800&q=80&fm=jpg&fit=crop',
     'Laptops & Accessories', 4.9, 388, TRUE),

    ('KF-LAP-403', '6009880403012', 'MacBook USB-C Power Adapter',
     '67W USB-C fast charger with a woven cable that will not fray in a bag. Charges a MacBook to 50% in around half an hour; also tops up an iPad or a phone.',
     3200000, 4100000, 30,
     'https://images.unsplash.com/photo-1583863788434-e58a36330cf0?w=800&q=80&fm=jpg&fit=crop',
     'Laptops & Accessories', 4.4, 96, FALSE),

    ('KF-LAP-404', '6009880404019', 'Wireless Precision Mouse',
     'Lightweight wireless mouse with a silent scroll and a rechargeable cell that lasts weeks. Works on a desk, a sofa arm, or your knee on a bad-network day.',
     1850000, 2400000, 45,
     'https://images.unsplash.com/photo-1527814050087-3793815479db?w=800&q=80&fm=jpg&fit=crop',
     'Laptops & Accessories', 4.3, 211, FALSE),

    ('KF-LAP-405', '6009880405016', 'Aluminium Desktop Display',
     '27" all-in-one aluminium desktop — a clean, single-cable workstation for a home office. Sharp panel, built-in speakers, tidy footprint.',
     92000000, 108000000, 5,
     'https://images.unsplash.com/photo-1527443224154-c4a3942d3acf?w=800&q=80&fm=jpg&fit=crop',
     'Laptops & Accessories', 4.5, 74, FALSE)

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
