-- Migration 20000611094600: extraction_attempts partial unique index (real DDL).
-- The honest restoration: the migration installs exactly the schema it claims.
CREATE UNIQUE INDEX IF NOT EXISTS extraction_jobs_active_uq
    ON sample.extraction_attempts (project_id)
    WHERE status = 'active';
