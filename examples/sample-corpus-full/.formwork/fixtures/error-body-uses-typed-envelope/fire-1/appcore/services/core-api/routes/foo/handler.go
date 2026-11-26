//go:build ignore

package foo

import "fmt"

func writeErr(w Writer) {
	fmt.Fprintf(w, `{"error":"bad request"}`) // want: error-body-uses-typed-envelope
}

func writeFault(w Writer) {
	_, _ = w.Write([]byte("{\"error\":\"nope\"}")) // want: error-body-uses-typed-envelope
}
