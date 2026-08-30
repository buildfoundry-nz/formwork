-- Registers only workflow_stage_events with pg_partman; palletra.annotations is
-- NOT handed to partman via create_parent, so on AlloyDB it has no forward
-- partition and 23514s at the quarter rollover (#2867 / #4708).
DO $$ BEGIN
  PERFORM public.create_parent(
    p_parent_table := 'palletra.workflow_stage_events',
    p_source_column := 'created_at',
    p_type := 'range',
    p_interval := '3 months',
    p_lead_partitions := 4);
END $$;
