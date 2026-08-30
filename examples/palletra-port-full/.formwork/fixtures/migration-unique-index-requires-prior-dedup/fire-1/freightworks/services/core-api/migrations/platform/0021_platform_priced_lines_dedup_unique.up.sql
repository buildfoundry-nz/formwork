-- Canonicalise the slug, then build the unique index. No intervening dedup:
-- rows the canonicalising write collapsed to byte-equal values collide (23505).
UPDATE platform.priced_lines SET slug = lower(trim(slug)); -- want: migration-unique-index-requires-prior-dedup

CREATE UNIQUE INDEX idx_platform_priced_lines_slug
    ON platform.priced_lines (slug);
