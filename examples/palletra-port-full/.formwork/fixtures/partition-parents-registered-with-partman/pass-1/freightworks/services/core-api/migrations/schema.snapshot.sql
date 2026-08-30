--
-- Committed schema snapshot (canonical end-state).
-- palletra.annotations is a RANGE-partitioned parent (seed range child below).
--

ALTER TABLE ONLY palletra.annotations ATTACH PARTITION palletra.markers_2026q2 FOR VALUES FROM ('2026-04-01 00:00:00+00') TO ('2026-07-01 00:00:00+00');
