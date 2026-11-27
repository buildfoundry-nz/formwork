-- Canonical schema snapshot (subset).
CREATE TYPE sample.item_type AS ENUM ('beam', 'column', 'slab');
CREATE DOMAIN platform.email AS text CHECK (VALUE ~ '@');
