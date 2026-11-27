//go:build ignore
//go:build integration

package orgstore

import (
	"context"
	"testing"

	"github.com/sample/appcore/internal/db"
	"github.com/sample/appcore/services/core-api/migrations"
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
