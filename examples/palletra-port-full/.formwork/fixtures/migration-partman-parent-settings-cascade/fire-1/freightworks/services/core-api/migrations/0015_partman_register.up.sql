-- Register workflow_stage_events with pg_partman.
DO $$
BEGIN
  PERFORM partman.create_parent(
    p_parent_table := 'palletra.workflow_stage_events',
    p_source_column      := 'created_at',
    p_type         := 'range',
    p_interval     := '1 month'
  );
END $$;
