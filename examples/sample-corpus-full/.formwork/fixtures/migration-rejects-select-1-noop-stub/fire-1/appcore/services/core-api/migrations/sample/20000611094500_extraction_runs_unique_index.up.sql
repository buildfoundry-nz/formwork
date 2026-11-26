-- Migration 20000611094500: extraction_attempts partial unique index.
-- Stubbed to a no-op after the source was reverted while the index stayed live
-- on dev — the exact source-of-truth divergence sweep-17 #1 pins.
SELECT 1; -- want: migration-rejects-select-1-noop-stub
