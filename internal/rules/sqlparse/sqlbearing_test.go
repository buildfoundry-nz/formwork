package sqlparse

import "testing"

// Each case is a composition's text as FromGo reassembles it, with fw_expr
// standing for the operand it could not read. The SQL half are shapes a
// measured corpus composes its queries in; the prose half are its log
// messages, filters, templates and command tags, every one of which the census
// used to call SQL it could not read.
func TestSQLBearingTellsComposedSQLFromProse(t *testing.T) {
	sql := []string{
		"SELECT id FROM t WHERE org_id = fw_expr",
		"SELECTfw_expr FROM takeoffqs.boqs b WHERE b.id = $1",
		"fw_exprSELECT a.code\n  FROM takeoffqs.annotations a",
		"fw_expr\n ORDER BY sod.n DESC\n LIMIT 1",
		" WHERE a.project_id = $1 AND fw_expr",
		" LIMIT $fw_expr",
		"fw_expr FOR NO KEY UPDATE OF p",
		"SELECT pg_advisory_xact_lock(fw_expr)",
		"SELECT count(*)fw_expr",
		"UPDATE takeoffqs.annotationsfw_expr",
		"SET LOCAL ROLE fw_expr",
		"RESET ROLE; GRANT SELECT ON fw_expr TO PUBLIC",
		"COPY fw_expr (version, dirty) FROM stdin;",
		"platform.priced_items pi\n  JOIN platform.price_sources ps ON ps.id = pi.price_source_id",
		"fw_expr, v.supplier_attribution\n\tFROM src v",
		"(o.name, o.id) > ($fw_expr, $fw_expr::uuid)",
		"$fw_expr::text[]",
		"o.assigned_estimator_user_id = fw_expr::uuid",
		"COALESCE(fw_expr, 0) > 0",
		"(event_kind = 'fw_expr' AND error_code IS NOT DISTINCT FROM 'fw_expr')",
		"AND (status <> 'fw_expr' OR $3 = 'fw_expr')",
		"fw_expr AND site_id = $3 RETURNING user_id",
		" AND EXISTS (SELECT 1 FROM fw_expr mem WHERE mem.id = s.id)",
		"$1fw_expr",
	}
	prose := []string{
		"could not reach fw_expr: fw_expr",
		"Nearly ready — fw_expr",
		"fw_expr/works?filter=fw_expr&select=DOI,abstract&rows=fw_expr",
		"You're invited to join fw_expr on TakeoffQS",
		"fw_expr and fw_expr",
		"resource.labels.service_name=fw_expr AND jsonPayload.kind=fw_expr AND timestamp>=fw_expr",
		"NOT CAPTURED: fw_expr\n",
		"arm2 allowlist: fw_expr is not a SELECT statement",
		"fw_expr: SET-NULL FK to platform.users alongside a PII column",
		"Priced in fw_expr — it will price $0 until coverage is added.",
		"- Low: $fw_expr — the floor everything measured supports today\n",
		"org spend NZ$fw_expr reached the NZ$fw_expr ceiling",
		"DELETE fw_expr",
		"#!/bin/sh\nexec \"$1\" \"$@\" fw_expr\n",
		"/// rejects distinctCount < fw_expr with ErrNotEnoughVertices, which the server validates as one.",
		"DO NOT EDIT. Generated from fw_expr.",
		"\\$fw_expr\\b",
	}
	for _, s := range sql {
		if !sqlBearing(s) {
			t.Errorf("sqlBearing(%q) = false; this composes SQL", s)
		}
	}
	for _, s := range prose {
		if sqlBearing(s) {
			t.Errorf("sqlBearing(%q) = true; this is not SQL", s)
		}
	}
}
