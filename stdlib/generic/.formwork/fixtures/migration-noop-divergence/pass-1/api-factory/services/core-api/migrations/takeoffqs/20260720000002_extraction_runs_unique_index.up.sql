-- Migration 20260720000002: extraction_runs partial unique index (real DDL).
-- The honest restoration: the migration installs exactly the schema it claims.
CREATE UNIQUE INDEX IF NOT EXISTS extraction_runs_active_uq
    ON takeoffqs.extraction_runs (project_id)
    WHERE status = 'active';
