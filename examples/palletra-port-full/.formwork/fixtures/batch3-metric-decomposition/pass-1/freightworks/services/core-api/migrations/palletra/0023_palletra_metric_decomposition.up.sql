ALTER TABLE palletra.page_gauges DROP COLUMN IF EXISTS principal_kind;
ALTER TABLE palletra.page_gauges DROP COLUMN IF EXISTS anno_kind;
-- complete cascade: every enum-typed column is dropped before the DROP TYPE.
DROP TYPE IF EXISTS palletra.metric_kind;
DROP TYPE IF EXISTS palletra.annotation_type;
