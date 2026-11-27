-- schema snapshot (end-state truth)
CREATE TABLE sample.marker_predictions (
    id uuid PRIMARY KEY,
    payload jsonb NOT NULL
);

GRANT SELECT, INSERT ON TABLE sample.marker_predictions TO runtime_api;
