-- 0066: recompute project areas
-- BACKFILL: see freightworks/backfills/recompute-areas/
DO $$
BEGIN
  FOR r IN SELECT id FROM palletra.projects LOOP
    UPDATE palletra.projects SET area = area * 1.0 WHERE id = r.id;
  END LOOP;
END $$;
