CREATE POLICY field_key_definitions_read ON sample.field_key_definitions FOR SELECT TO runtime_api USING (true);
CREATE POLICY organization_types_scoped ON sample.organization_types FOR ALL TO runtime_api USING ((org_id = current_setting('app.active_org_id', true)::uuid)) WITH CHECK ((org_id = current_setting('app.active_org_id', true)::uuid));
CREATE POLICY account_removals_no_tenant_access ON sample.account_removals FOR ALL TO runtime_api USING (false) WITH CHECK (false);
CREATE POLICY capabilities_view ON sample.capabilities FOR SELECT TO runtime_api USING (true);
