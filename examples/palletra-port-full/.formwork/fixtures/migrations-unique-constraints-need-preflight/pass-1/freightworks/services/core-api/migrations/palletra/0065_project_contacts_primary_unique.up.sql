-- 0065_project_contacts_primary_unique.up.sql
-- Preflight runs first: it errors on any offending duplicate set before the
-- unique index is built, so operators get the offending rows, not a 23505.
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM palletra.project_stakeholders
     GROUP BY project_id, stakeholder_type
    HAVING count(*) > 1
  ) THEN
    RAISE EXCEPTION 'duplicate primary parties exist; resolve before applying';
  END IF;
END $$;

CREATE UNIQUE INDEX project_contacts_primary_per_side ON palletra.project_stakeholders (project_id, stakeholder_type);
