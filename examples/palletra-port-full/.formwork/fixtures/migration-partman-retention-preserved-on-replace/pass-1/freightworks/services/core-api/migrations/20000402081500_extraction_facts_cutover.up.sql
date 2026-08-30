-- Greenfield extraction-facts cutover.
-- RETENTION-DROPPED-BY-DESIGN(palletra.sku_extraction_runs): ML replay
-- keeps facts forever; see docs/plans/phase-6-decisions-log.md.
DROP TABLE palletra.skus_extraction_runs;

DO $$
BEGIN
  PERFORM partman.create_parent(
    p_parent_table := 'palletra.sku_extraction_runs',
    p_source_column      := 'created_at',
    p_type         := 'range',
    p_interval     := '1 month'
  );
END $$;
