package facet

import (
	"encoding/binary"
	"unicode/utf16"
	"unicode/utf8"
)

type textWidth uint8

const (
	textI8 textWidth = iota + 1
	textI16
	textI32
)

func encodeText(value string, width textWidth, wtf int32) ([]byte, uint64, int32) {
	if wtf != 0 && wtf != 1 {
		return nil, 0, ErrInvalid
	}
	if wtf == 0 && !utf8.ValidString(value) {
		return nil, 0, ErrIllegalSequence
	}

	codepoints := decodeTextCodepoints(value, wtf != 0)
	if codepoints == nil && value != "" {
		return nil, 0, ErrIllegalSequence
	}

	switch width {
	case textI8:
		if wtf == 0 {
			out := append([]byte(nil), value...)
			return out, uint64(len(out)), ErrOK
		}
		out := make([]byte, 0, len(value))
		for _, r := range codepoints {
			out = appendWTF8(out, r)
		}
		return out, uint64(len(out)), ErrOK
	case textI16:
		units := make([]uint16, 0, len(codepoints))
		for _, r := range codepoints {
			if r >= 0xd800 && r <= 0xdfff {
				if wtf == 0 {
					return nil, 0, ErrIllegalSequence
				}
				units = append(units, uint16(r))
				continue
			}
			if r < 0 || r > utf8.MaxRune {
				return nil, 0, ErrIllegalSequence
			}
			if r <= 0xffff {
				units = append(units, uint16(r))
				continue
			}
			hi, lo := utf16.EncodeRune(r)
			units = append(units, uint16(hi), uint16(lo))
		}
		out := make([]byte, len(units)*2)
		for i, unit := range units {
			binary.LittleEndian.PutUint16(out[i*2:], unit)
		}
		return out, uint64(len(units)), ErrOK
	case textI32:
		out := make([]byte, len(codepoints)*4)
		for i, r := range codepoints {
			if r < 0 || r > utf8.MaxRune || (wtf == 0 && r >= 0xd800 && r <= 0xdfff) {
				return nil, 0, ErrIllegalSequence
			}
			binary.LittleEndian.PutUint32(out[i*4:], uint32(r))
		}
		return out, uint64(len(codepoints)), ErrOK
	default:
		return nil, 0, ErrInvalid
	}
}

func decodeTextCodepoints(value string, allowSentinels bool) []rune {
	if !allowSentinels {
		if !utf8.ValidString(value) {
			return nil
		}
		return []rune(value)
	}
	out := make([]rune, 0, len(value))
	for len(value) != 0 {
		r, size := utf8.DecodeRuneInString(value)
		if r == utf8.RuneError && size == 1 {
			b := value[0]
			if b < 0x80 {
				return nil
			}
			out = append(out, rune(0xdc00)+rune(b))
			value = value[1:]
			continue
		}
		out = append(out, r)
		value = value[size:]
	}
	return out
}

func appendWTF8(dst []byte, r rune) []byte {
	if r >= 0xd800 && r <= 0xdfff {
		return append(dst,
			byte(0xe0|uint32(r)>>12),
			byte(0x80|(uint32(r)>>6)&0x3f),
			byte(0x80|uint32(r)&0x3f),
		)
	}
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], r)
	return append(dst, buf[:n]...)
}
