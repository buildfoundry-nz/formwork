//go:build ignore

package bom

const readViewSnapshotSQL = `SELECT COALESCE(view_settings_snapshot::text, '{}')::bytea FROM palletra.projects WHERE id = $1` // want: sole-display-settings-reader
