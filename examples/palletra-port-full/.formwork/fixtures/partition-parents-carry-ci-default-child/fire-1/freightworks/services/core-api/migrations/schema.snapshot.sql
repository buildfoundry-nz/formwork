--
-- Committed schema snapshot (canonical end-state).
-- palletra.annotations is a RANGE-partitioned parent (its seed range child is
-- attached below) but no DEFAULT catch-all partition is attached, so the
-- no-partman CI DB 23514s at the next date rollover (#8004).
--

ALTER TABLE ONLY palletra.annotations ATTACH PARTITION palletra.markers_2026q2 FOR VALUES FROM ('2026-04-01 00:00:00+00') TO ('2026-07-01 00:00:00+00');
