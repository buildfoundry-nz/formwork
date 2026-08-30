-- Register the plural extraction-runs telemetry table and age it out at 12mo.
DO $$
BEGIN
  PERFORM partman.create_parent(
    p_parent_table := 'palletra.skus_extraction_runs',
    p_source_column      := 'created_at',
    p_type         := 'range',
    p_interval     := '1 month'
  );
  UPDATE public.part_config
     SET retention = '12 months', retention_preserve_table = false
   WHERE parent_table = 'palletra.skus_extraction_runs';
END $$;
