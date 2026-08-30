-- Legitimate create+drop history: the migrations/ tree is excluded from the ban.
ALTER TABLE palletra.annotation_gauges DROP COLUMN derived_from_annotation_ids;
