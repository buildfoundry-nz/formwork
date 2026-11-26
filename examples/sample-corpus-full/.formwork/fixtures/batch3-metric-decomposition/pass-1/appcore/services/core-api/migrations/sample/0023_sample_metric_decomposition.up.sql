ALTER TABLE sample.page_gauges DROP COLUMN IF EXISTS principal_kind;
ALTER TABLE sample.page_gauges DROP COLUMN IF EXISTS anno_kind;
-- complete cascade: every enum-typed column is dropped before the DROP TYPE.
DROP TYPE IF EXISTS sample.metric_kind;
DROP TYPE IF EXISTS sample.annotation_type;
