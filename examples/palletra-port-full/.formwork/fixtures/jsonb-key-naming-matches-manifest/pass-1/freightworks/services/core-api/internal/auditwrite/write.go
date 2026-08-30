//go:build ignore

package auditwrite

import (
	"context"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
)

// WriteAuditEvent persists an audit event's metadata jsonb column.
// docs/jsonb-key-naming-matches-manifests.md declares palletra.audit_events.metadata as
// use_proto_names=true; this writer marshals with UseProtoNames: true so the
// snake_case keys match what the stats reader filters on.
func WriteAuditEvent(ctx context.Context, tx pgx.Tx, meta *AuditMeta) error {
	// NB: never marshal this column with UseProtoNames: false — the comment
	// mentioning the wrong setting must not trip the comment-stripped gate.
	m := protojson.MarshalOptions{UseProtoNames: true}
	raw, err := m.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO palletra.audit_events (metadata) VALUES ($1)`, raw)
	return err
}
