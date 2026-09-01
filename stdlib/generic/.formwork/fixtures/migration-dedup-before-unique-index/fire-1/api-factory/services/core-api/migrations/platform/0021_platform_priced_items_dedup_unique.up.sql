-- Canonicalise the slug, then build the unique index. No intervening dedup:
-- rows the canonicalising write collapsed to byte-equal values collide (23505).
UPDATE platform.priced_items SET slug = lower(trim(slug)); -- want: migration-dedup-before-unique-index

CREATE UNIQUE INDEX idx_platform_priced_items_slug
    ON platform.priced_items (slug);
