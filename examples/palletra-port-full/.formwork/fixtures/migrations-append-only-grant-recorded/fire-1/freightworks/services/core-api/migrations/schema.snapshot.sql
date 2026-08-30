-- schema snapshot (end-state truth)
CREATE TABLE palletra.marker_predictions (
    id uuid PRIMARY KEY,
    payload jsonb NOT NULL
);

-- No GRANT line for the append-only table is present; a missing grant fails.
