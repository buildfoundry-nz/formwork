//go:build ignore

package projectfeed

func decode(b []byte) (*structpb.Struct, error) {
	dst := &structpb.Struct{}
	if err := protojson.Unmarshal(b, dst); err != nil {
		return nil, err
	}
	return dst, nil
}
