CREATE POLICY field_key_definitions_read ON palletra.field_key_definitions FOR SELECT TO runtime_api USING (true);
CREATE POLICY organization_types_scoped ON palletra.organization_types FOR ALL TO runtime_api USING ((org_id = current_setting('app.active_org_id', true)::uuid)) WITH CHECK ((org_id = current_setting('app.active_org_id', true)::uuid));
CREATE POLICY account_removals_no_tenant_access ON palletra.account_removals FOR ALL TO runtime_api USING (false) WITH CHECK (false);
CREATE POLICY field_key_definitions_write ON palletra.field_key_definitions FOR ALL TO runtime_api USING (true) WITH CHECK (true); -- want: no-unrestricted-platform-api-write-policy
