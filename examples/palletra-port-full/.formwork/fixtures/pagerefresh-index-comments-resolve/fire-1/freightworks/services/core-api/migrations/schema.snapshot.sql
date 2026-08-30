--
-- Committed schema snapshot (canonical end-state).
--

CREATE INDEX idx_notes_org_type ON ONLY palletra.annotations USING btree (org_id, annotation_type, created_at DESC);
CREATE INDEX idx_gauges_org_calc ON ONLY palletra.page_gauges USING btree (org_id, calc_version);
