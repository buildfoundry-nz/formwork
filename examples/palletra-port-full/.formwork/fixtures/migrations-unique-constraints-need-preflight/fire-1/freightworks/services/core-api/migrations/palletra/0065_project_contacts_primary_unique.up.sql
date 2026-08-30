-- 0065_project_contacts_primary_unique.up.sql
-- Missing preflight: the unique index is created before any duplicate-detection
-- guard, so a mid-deploy 23505 surfaces as an opaque pgx error.
CREATE UNIQUE INDEX project_contacts_primary_per_side ON palletra.project_stakeholders (project_id, stakeholder_type); -- want: migrations-unique-constraints-need-preflight

DO $$
BEGIN
  RAISE EXCEPTION 'duplicate primary parties exist; resolve before applying';
END $$;
