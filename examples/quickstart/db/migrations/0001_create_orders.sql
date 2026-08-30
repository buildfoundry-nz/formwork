-- Create the orders table.
--
-- Passes migrations-are-transactional: this migration alters a table, so it is
-- wrapped in a transaction. A failure part-way leaves the schema unchanged.
BEGIN;

CREATE TABLE orders (
    id     text PRIMARY KEY,
    status text NOT NULL
);

ALTER TABLE orders
    ADD CONSTRAINT orders_status_known
    CHECK (status IN ('pending', 'shipped', 'cancelled'));

COMMIT;
