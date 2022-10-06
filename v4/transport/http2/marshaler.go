package http2

import (
	"encoding/gob"
	"io"
)

type mUnmarshaler interface {
	Marshal(any) error
	Unmarshal(any) error
}

type gobMUnmarshaler struct {
	enc *gob.Encoder
	dec *gob.Decoder
}

func NewGobMUnmarshaler(r io.Reader, w io.Writer) (*gobMUnmarshaler, error) {
	return &gobMUnmarshaler{
		enc: gob.NewEncoder(w),
		dec: gob.NewDecoder(r),
	}, nil
}

func (g *gobMUnmarshaler) Marshal(v any) error {
	return g.enc.Encode(v)
}

func (g *gobMUnmarshaler) Unmarshal(v any) error {
	return g.dec.Decode(v)
}
