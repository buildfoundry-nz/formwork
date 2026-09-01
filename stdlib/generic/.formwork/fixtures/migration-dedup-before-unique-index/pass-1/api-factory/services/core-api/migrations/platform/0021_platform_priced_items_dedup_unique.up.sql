-- Canonicalise the slug, dedup the collisions the UPDATE created, THEN index.
UPDATE platform.priced_items SET slug = lower(trim(slug));

DELETE FROM platform.priced_items a
      USING platform.priced_items b
      WHERE a.slug = b.slug
        AND a.id > b.id;

CREATE UNIQUE INDEX idx_platform_priced_items_slug
    ON platform.priced_items (slug);
