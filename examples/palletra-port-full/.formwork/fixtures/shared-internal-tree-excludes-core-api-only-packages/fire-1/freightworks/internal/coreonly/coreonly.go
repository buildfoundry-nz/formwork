//go:build ignore

package coreonly

// Parked in the SHARED tree (freightworks/internal/) but imported only from
// services/core-api/ below — core-api-only code masquerading as shared (#1962).
func Value() int { return 42 }
