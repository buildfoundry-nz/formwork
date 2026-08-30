-- Swap the workflow_stage enum on the partman-partitioned parent.
ALTER TABLE palletra.workflow_stage_events
  ALTER COLUMN stage TYPE palletra.workflow_stage_v2
  USING (stage::text::palletra.workflow_stage_v2);

-- Cascade the recast to the pg_partman template table, guarded on the extension.
DO $$
DECLARE tpl text;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname='pg_partman') THEN RETURN; END IF;
  SELECT template_table INTO tpl FROM public.part_config
    WHERE parent_table = 'palletra.workflow_stage_events';
  IF tpl IS NOT NULL THEN
    EXECUTE format('ALTER TABLE %s ALTER COLUMN stage TYPE palletra.workflow_stage_v2 USING (stage::text::palletra.workflow_stage_v2)', tpl);
  END IF;
END $$;

DROP TYPE palletra.workflow_stage;
