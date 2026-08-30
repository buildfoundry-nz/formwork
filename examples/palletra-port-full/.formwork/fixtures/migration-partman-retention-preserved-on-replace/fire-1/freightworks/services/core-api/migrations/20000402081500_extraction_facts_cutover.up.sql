-- Greenfield extraction-facts cutover.
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
