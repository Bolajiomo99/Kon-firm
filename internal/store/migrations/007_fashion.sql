-- More fashion.
--
-- Every image URL below was checked for HTTP 200 with an image content-type
-- before being committed. A guessed photo id renders as a broken card and is
-- indistinguishable from working code when read.

INSERT INTO products
    (sku, barcode, name, description, price_kobo, compare_at_kobo, stock,
     image_url, category, rating, review_count, is_new)
VALUES
    ('KF-FSH-107', '6009880107019', 'Oxford Cotton Shirt',
     'Long-staple cotton with a soft collar that holds its shape through a Lagos afternoon. Cut with room through the chest, tapered at the waist. Machine washable.',
     2450000, 3200000, 34,
     'https://images.unsplash.com/photo-1594633312681-425c7b97ccd1?w=800&q=80&fm=jpg&fit=crop',
     'Fashion', 4.5, 267, TRUE),

    ('KF-FSH-108', '6009880108016', 'Selvedge Denim Jacket',
     '13oz raw selvedge denim from a shuttle loom. Stiff at first, then it fades to the shape of whoever wears it. Copper rivets, chain-stitched hem.',
     5900000, 7800000, 16,
     'https://images.unsplash.com/photo-1544022613-e87ca75a784a?w=800&q=80&fm=jpg&fit=crop',
     'Fashion', 4.7, 189, FALSE),

    ('KF-FSH-109', '6009880109013', 'Merino Crew Knit',
     'Fine-gauge merino that breathes in heat and holds warmth in air conditioning. No itch, no pilling. The one jumper that works year-round in Lagos.',
     3800000, NULL, 22,
     'https://images.unsplash.com/photo-1596755094514-f87e34085b2c?w=800&q=80&fm=jpg&fit=crop',
     'Fashion', 4.6, 145, FALSE),

    ('KF-FSH-110', '6009880110019', 'Tailored Wool Trousers',
     'Half-canvassed construction with a clean break over the shoe. Wool tropical weave — it holds a crease without holding heat.',
     4600000, 6100000, 19,
     'https://images.unsplash.com/photo-1473966968600-fa801b869a1a?w=800&q=80&fm=jpg&fit=crop',
     'Fashion', 4.4, 98, FALSE),

    ('KF-FSH-111', '6009880111016', 'Linen Camp Shirt',
     'Washed European linen with a relaxed camp collar. Wrinkles on purpose — that is what linen does, and it is why it is cool.',
     2200000, 2900000, 41,
     'https://images.unsplash.com/photo-1618354691373-d851c5c3a990?w=800&q=80&fm=jpg&fit=crop',
     'Fashion', 4.3, 312, TRUE),

    ('KF-FSH-112', '6009880112013', 'Silk Scarf, Hand-Rolled',
     'Mulberry silk twill with hand-rolled edges. 90cm square. The print is screen-printed in eight passes, one colour at a time.',
     3100000, NULL, 28,
     'https://images.unsplash.com/photo-1521572163474-6864f9cf17ab?w=800&q=80&fm=jpg&fit=crop',
     'Fashion', 4.8, 76, FALSE),

    ('KF-FSH-113', '6009880113010', 'Leather Chelsea Boot',
     'Elastic-gusset Chelsea in full-grain calf on a Goodyear welt. Resoleable, so it outlives the trend. Breaks in within a week.',
     7800000, 9500000, 11,
     'https://images.unsplash.com/photo-1543163521-1bf539c55dd2?w=800&q=80&fm=jpg&fit=crop',
     'Fashion', 4.7, 154, FALSE),

    ('KF-ACC-303', '6009880303015', 'Woven Leather Belt',
     'Full-grain woven leather with a solid brass buckle. No holes — the weave adjusts anywhere, so it fits before and after lunch.',
     1650000, 2100000, 37,
     'https://images.unsplash.com/photo-1553062407-98eeb64c6a62?w=800&q=80&fm=jpg&fit=crop',
     'Accessories', 4.2, 203, FALSE)

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
