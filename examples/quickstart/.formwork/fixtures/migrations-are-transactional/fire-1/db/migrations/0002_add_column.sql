-- Add a shipped_at column. No transaction: if the UPDATE fails, the column is
-- already added and the migration cannot simply be re-run.
ALTER TABLE orders ADD COLUMN shipped_at timestamptz; -- want: migrations-are-transactional

UPDATE orders SET shipped_at = now() WHERE status = 'shipped';
