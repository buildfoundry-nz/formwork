//go:build ignore

package stockroom

func build(size int) ParsedSheet {
	return ParsedSheet{RawBytes: size}
}
