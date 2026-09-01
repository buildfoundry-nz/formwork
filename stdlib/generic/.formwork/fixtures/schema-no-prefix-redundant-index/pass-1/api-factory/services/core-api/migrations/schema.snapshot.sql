CREATE INDEX idx_metrics_project_page ON takeoffqs.metrics USING btree (project_id, page_id);
