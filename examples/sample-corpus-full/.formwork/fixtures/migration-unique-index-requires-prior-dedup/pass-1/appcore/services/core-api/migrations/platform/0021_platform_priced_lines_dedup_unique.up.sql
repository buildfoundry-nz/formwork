-- Canonicalise the slug, dedup the collisions the UPDATE created, THEN index.
UPDATE platform.priced_lines SET slug = lower(trim(slug));

DELETE FROM platform.priced_lines a
      USING platform.priced_lines b
      WHERE a.slug = b.slug
        AND a.id > b.id;

CREATE UNIQUE INDEX idx_platform_priced_lines_slug
    ON platform.priced_lines (slug);
