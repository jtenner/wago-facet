package facet

import (
	"bytes"
	"testing"
)

func FuzzTextCodecRoundTrip(f *testing.F) {
	seeds := [][]byte{
		{},
		[]byte("facet"),
		{0xed, 0xb2, 0x80},
		{0x00, 0xd8},
		{0x80, 0xdc},
		{0x00, 0x00, 0x01, 0x00},
		{0xff, 0xff, 0xff, 0xff},
	}
	for _, seed := range seeds {
		f.Add(seed, uint8(textI8), int32(0))
		f.Add(seed, uint8(textI8), int32(1))
		f.Add(seed, uint8(textI16), int32(0))
		f.Add(seed, uint8(textI16), int32(1))
		f.Add(seed, uint8(textI32), int32(0))
		f.Add(seed, uint8(textI32), int32(1))
	}

	f.Fuzz(func(t *testing.T, raw []byte, rawWidth uint8, wtf int32) {
		width := textWidth(rawWidth)
		decoded, code := decodeTextBytes(raw, width, wtf)
		if code != ErrOK {
			return
		}
		encoded, _, encodeCode := encodeText(decoded, width, wtf)
		if encodeCode != ErrOK {
			t.Fatalf("successful decode failed to encode: width=%d wtf=%d code=%d", width, wtf, encodeCode)
		}
		if !bytes.Equal(encoded, raw) {
			t.Fatalf("text round trip changed bytes: width=%d wtf=%d got=%x want=%x", width, wtf, encoded, raw)
		}
	})
}

func FuzzPluginConfigValidation(f *testing.F) {
	for _, seed := range [][]byte{
		{},
		[]byte(`{}`),
		[]byte(`null`),
		[]byte(`{"stdin":"eof","preopens":[]}`),
		[]byte(`{"preopens":[{"guest":"/","host":"/tmp","rights":[]}]}`),
		[]byte(`{"maxHandles":1024}`),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(_ *testing.T, raw []byte) {
		_ = validatePluginConfig(raw)
	})
}
