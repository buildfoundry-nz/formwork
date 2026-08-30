//go:build ignore

package auditwrite

import (
	"context"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
)

// WriteAuditEvent persists an audit event's metadata jsonb column.
// docs/jsonb-key-naming-matches-manifests.md declares palletra.audit_events.metadata as
// use_proto_names=true (snake_case keys) — but this writer marshals with
// UseProtoNames: false, storing camelCase keys the stats reader never finds.
func WriteAuditEvent(ctx context.Context, tx pgx.Tx, meta *AuditMeta) error {
	m := protojson.MarshalOptions{UseProtoNames: false}
	raw, err := m.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO palletra.audit_events (metadata) VALUES ($1)`, raw)
	return err
}
