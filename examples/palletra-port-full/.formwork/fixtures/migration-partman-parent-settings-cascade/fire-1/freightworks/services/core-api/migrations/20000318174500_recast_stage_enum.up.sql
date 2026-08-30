-- Swap the workflow_stage enum on the partman-partitioned parent.
ALTER TABLE palletra.workflow_stage_events
  ALTER COLUMN stage TYPE palletra.workflow_stage_v2
  USING (stage::text::palletra.workflow_stage_v2);

DROP TYPE palletra.workflow_stage;
