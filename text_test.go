package facet

import "testing"

func TestEncodeTextStrictAndWTF(t *testing.T) {
	if _, units, code := encodeText("caf\u00e9", textI8, 0); code != ErrOK || units != 5 {
		t.Fatalf("strict i8 units=%d code=%d", units, code)
	}
	if _, units, code := encodeText("\U0001f600", textI16, 0); code != ErrOK || units != 2 {
		t.Fatalf("strict i16 units=%d code=%d", units, code)
	}
	if _, units, code := encodeText("\U0001f600", textI32, 0); code != ErrOK || units != 1 {
		t.Fatalf("strict i32 units=%d code=%d", units, code)
	}

	invalid := string([]byte{'a', 0xff, 'b'})
	if _, _, code := encodeText(invalid, textI8, 0); code != ErrIllegalSequence {
		t.Fatalf("strict invalid code=%d", code)
	}
	bytes, units, code := encodeText(invalid, textI8, 1)
	if code != ErrOK || units != 5 || len(bytes) != 5 {
		t.Fatalf("WTF-8 bytes=%x units=%d code=%d", bytes, units, code)
	}
	if _, units, code := encodeText(invalid, textI16, 1); code != ErrOK || units != 3 {
		t.Fatalf("WTF-16 units=%d code=%d", units, code)
	}
	if _, units, code := encodeText(invalid, textI32, 1); code != ErrOK || units != 3 {
		t.Fatalf("WTF-32 units=%d code=%d", units, code)
	}
	if _, _, code := encodeText("x", textI8, 2); code != ErrInvalid {
		t.Fatalf("invalid wtf code=%d", code)
	}
}
