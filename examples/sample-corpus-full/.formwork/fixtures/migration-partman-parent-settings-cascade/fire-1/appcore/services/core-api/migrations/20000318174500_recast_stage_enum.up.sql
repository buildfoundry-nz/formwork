-- Swap the workflow_stage enum on the partman-partitioned parent.
ALTER TABLE sample.workflow_stage_events
  ALTER COLUMN stage TYPE sample.workflow_stage_v2
  USING (stage::text::sample.workflow_stage_v2);

DROP TYPE sample.workflow_stage;
