-- Add a shipped_at column, transactionally.
BEGIN;

ALTER TABLE orders ADD COLUMN shipped_at timestamptz;

UPDATE orders SET shipped_at = now() WHERE status = 'shipped';

COMMIT;
