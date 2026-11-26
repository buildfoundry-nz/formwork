CREATE TABLE sample.widgets (
  id uuid PRIMARY KEY,
  org_id uuid NOT NULL,
  name text NOT NULL
);
ALTER TABLE sample.widgets ENABLE ROW LEVEL SECURITY;
CREATE POLICY widgets_tenant_read ON sample.widgets
  AS PERMISSIVE FOR ALL TO runtime_api
  USING (org_id = current_setting('app.active_org_id', true)::uuid)
  WITH CHECK (org_id = current_setting('app.active_org_id', true)::uuid);
