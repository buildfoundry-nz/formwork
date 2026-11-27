ALTER TABLE sample.page_gauges DROP COLUMN IF EXISTS principal_kind;
-- incomplete cascade: the second enum column is left behind, so the
-- trailing DROP TYPE below will fail at deploy time.
DROP TYPE IF EXISTS sample.metric_kind;
DROP TYPE IF EXISTS sample.annotation_type;
