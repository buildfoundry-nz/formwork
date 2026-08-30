//go:build ignore

package workerharness

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

var ErrPhaseEnumMissing = errors.New("page-readiness: stage enum value missing")

func countPreparedPages() error {
	var pgErr *pgconn.PgError
	if errors.As(runErr, &pgErr) && pgErr.Code == "22P02" {
		return ErrPhaseEnumMissing
	}
	return nil
}
