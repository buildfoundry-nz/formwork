--
-- Committed schema snapshot (canonical end-state).
-- takeoffqs.annotations is RANGE-partitioned and carries a DEFAULT catch-all
-- partition, so the no-partman CI DB routes rows at any date (#5319).
--

ALTER TABLE ONLY takeoffqs.annotations ATTACH PARTITION takeoffqs.annotations_2026q2 FOR VALUES FROM ('2026-04-01 00:00:00+00') TO ('2026-07-01 00:00:00+00');
ALTER TABLE ONLY takeoffqs.annotations ATTACH PARTITION takeoffqs.annotations_default DEFAULT;
