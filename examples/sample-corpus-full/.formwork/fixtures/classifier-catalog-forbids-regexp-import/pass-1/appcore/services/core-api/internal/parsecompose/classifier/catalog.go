//go:build ignore

package classifier

// Page-type semantics are declarative catalog data (SimilarTo), never a
// regex post-process layer.
type Catalog struct {
	SimilarTo []string
}
