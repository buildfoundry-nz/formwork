CREATE TABLE sample.marker_predictions (id uuid PRIMARY KEY);
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE sample.marker_predictions TO runtime_api; -- want: migrations-forbid-mutate-grant-on-append-only-table
