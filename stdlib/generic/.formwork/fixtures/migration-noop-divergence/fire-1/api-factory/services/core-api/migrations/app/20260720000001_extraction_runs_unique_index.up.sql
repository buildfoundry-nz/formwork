-- Migration 20260720000001: extraction_runs partial unique index.
-- Stubbed to a no-op after the source was reverted while the index stayed live
-- on dev — the exact source-of-truth divergence audit-17 #1 pins.
SELECT 1; -- want: migration-noop-divergence
