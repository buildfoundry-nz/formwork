//go:build ignore
//go:build integration

package orgstore

import (
	"context"
	"testing"

	"github.com/palletra/freightworks/internal/db"
	"github.com/palletra/freightworks/services/core-api/migrations"
)

func TestOrgStore_Integration(t *testing.T) {
	dsn := harnessDSN(t)
	if err := migrations.Run(dsn); err != nil { // want: m06-integration-tests-use-harness
		t.Fatal(err)
	}
	pool, err := db.NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	_ = pool
}
