-- Registers palletra.annotations with pg_partman via create_parent, so AlloyDB
-- has forward partitions past the last hardcoded child (#2867).
DO $$ BEGIN
  PERFORM public.create_parent(
    p_parent_table := 'palletra.annotations',
    p_source_column := 'created_at',
    p_type := 'range',
    p_interval := '3 months',
    p_lead_partitions := 4);
END $$;
