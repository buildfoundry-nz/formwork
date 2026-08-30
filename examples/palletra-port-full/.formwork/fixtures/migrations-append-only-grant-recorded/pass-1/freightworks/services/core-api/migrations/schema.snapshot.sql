-- schema snapshot (end-state truth)
CREATE TABLE palletra.marker_predictions (
    id uuid PRIMARY KEY,
    payload jsonb NOT NULL
);

GRANT SELECT, INSERT ON TABLE palletra.marker_predictions TO runtime_api;
