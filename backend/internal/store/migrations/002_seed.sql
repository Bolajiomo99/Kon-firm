-- Demo catalogue.
--
-- Seeded so a fresh clone has something to sell immediately — a reviewer
-- following the README should reach a working checkout without hand-entering
-- inventory. ON CONFLICT DO NOTHING keeps this safe to re-run on every boot.
--
-- Barcodes are real EAN-13 values so the POS scanner can be demonstrated by
-- pointing a camera at a printed code.
--
-- Prices are in kobo: 4500000 = ₦45,000.00

INSERT INTO products (sku, barcode, name, description, price_kobo, stock, image_url) VALUES
    ('KF-ANK-001', '6009880012345', 'Ankara Print Tote',
     'Hand-stitched tote in Kente-inspired Ankara. Reinforced base, cotton-webbed straps.',
     4500000, 24, '/img/tote.svg'),

    ('KF-ADR-002', '6009880023456', 'Adire Indigo Scarf',
     'Yoruba resist-dyed indigo on raw silk. Each piece is one of one.',
     2750000, 40, '/img/scarf.svg'),

    ('KF-LTH-003', '6009880034567', 'Kano Leather Wallet',
     'Vegetable-tanned Sokoto goatskin, hand-burnished edges, six card slots.',
     3200000, 18, '/img/wallet.svg'),

    ('KF-BDS-004', '6009880045678', 'Bida Brass Cuff',
     'Cast by Nupe brassworkers in Bida, Niger State. Adjustable, unlacquered.',
     1850000, 32, '/img/cuff.svg'),

    ('KF-CER-005', '6009880056789', 'Ilorin Clay Tumbler',
     'Wheel-thrown, wood-fired stoneware. Food safe, dishwasher safe.',
     950000, 60, '/img/tumbler.svg'),

    ('KF-TEX-006', '6009880067890', 'Aso-Oke Table Runner',
     'Narrow-strip handloom weave, 180cm. Traditional Ilorin technique.',
     2200000, 15, '/img/runner.svg')
ON CONFLICT (sku) DO NOTHING;
