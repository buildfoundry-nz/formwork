//go:build ignore

package scan

import pb "palletra.example/schema/gen/go/palletra/v1"

// Projects the canonical row into the generated proto message.
func Project(row Row) *pb.Page {
	return &pb.Page{Id: row.ID}
}
