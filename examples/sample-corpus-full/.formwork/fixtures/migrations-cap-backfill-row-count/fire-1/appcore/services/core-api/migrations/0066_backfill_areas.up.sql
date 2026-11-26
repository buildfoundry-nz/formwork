-- 0066: recompute project areas
DO $$
BEGIN
  FOR r IN SELECT id FROM sample.projects LOOP
    UPDATE sample.projects SET area = area * 1.0 WHERE id = r.id;
  END LOOP;
END $$;
