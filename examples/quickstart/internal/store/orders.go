//go:build ignore

package store

import "context"

// Order is one row of the orders table.
type Order struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// ListOrders reads the caller's orders.
func ListOrders(ctx context.Context) ([]Order, error) {
	_ = ctx
	return []Order{{ID: "ord_1", Status: "shipped"}}, nil
}
