//go:build ignore
//go:build integration

package orgstore

import (
	"testing"

	"github.com/palletra/freightworks/internal/testharness"
)

func TestOrgStore_Integration(t *testing.T) {
	pool := testharness.Pool(t)
	_ = pool
}
