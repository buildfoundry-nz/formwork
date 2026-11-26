CREATE INDEX idx_metrics_project ON app.metrics USING btree (project_id); -- want: schema-no-prefix-redundant-index
