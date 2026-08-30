ALTER TABLE palletra.page_gauges DROP COLUMN IF EXISTS principal_kind;
-- incomplete cascade: the second enum column is left behind, so the
-- trailing DROP TYPE below will fail at deploy time.
DROP TYPE IF EXISTS palletra.metric_kind;
DROP TYPE IF EXISTS palletra.annotation_type;
