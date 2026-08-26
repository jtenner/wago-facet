package facet

import (
	"encoding/binary"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	wago "github.com/wago-org/wago"
)

// decodeTextBytes converts one Facet text representation into the host's native
// byte-string form. Linux path namespaces are byte strings, so WTF mode maps
// U+DC80..U+DCFF back to the corresponding non-UTF-8 byte. DNS performs an
// additional ASCII-only validation after decoding.
func decodeTextBytes(raw []byte, width textWidth, wtf int32) (string, int32) {
	if !validWTF(wtf) {
		return "", ErrInvalid
	}
	var codepoints []rune
	switch width {
	case textI8:
		if wtf == 0 {
			if !utf8.Valid(raw) {
				return "", ErrIllegalSequence
			}
			return string(raw), ErrOK
		}
		for len(raw) != 0 {
			r, n := decodeWTF8Rune(raw)
			if n == 0 {
				return "", ErrIllegalSequence
			}
			codepoints = append(codepoints, r)
			raw = raw[n:]
		}
	case textI16:
		if len(raw)%2 != 0 {
			return "", ErrIllegalSequence
		}
		units := make([]uint16, len(raw)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(raw[i*2:])
		}
		for i := 0; i < len(units); i++ {
			u := units[i]
			if u >= 0xd800 && u <= 0xdbff {
				if i+1 < len(units) && units[i+1] >= 0xdc00 && units[i+1] <= 0xdfff {
					codepoints = append(codepoints, utf16.DecodeRune(rune(u), rune(units[i+1])))
					i++
					continue
				}
				if wtf == 0 {
					return "", ErrIllegalSequence
				}
				codepoints = append(codepoints, rune(u))
				continue
			}
			if u >= 0xdc00 && u <= 0xdfff {
				if wtf == 0 {
					return "", ErrIllegalSequence
				}
				codepoints = append(codepoints, rune(u))
				continue
			}
			codepoints = append(codepoints, rune(u))
		}
	case textI32:
		if len(raw)%4 != 0 {
			return "", ErrIllegalSequence
		}
		codepoints = make([]rune, 0, len(raw)/4)
		for i := 0; i < len(raw); i += 4 {
			r := rune(binary.LittleEndian.Uint32(raw[i:]))
			if r < 0 || r > utf8.MaxRune {
				return "", ErrIllegalSequence
			}
			if r >= 0xd800 && r <= 0xdfff && wtf == 0 {
				return "", ErrIllegalSequence
			}
			codepoints = append(codepoints, r)
		}
	default:
		return "", ErrInvalid
	}

	var out strings.Builder
	for _, r := range codepoints {
		if r >= 0xd800 && r <= 0xdfff {
			if wtf == 0 || r < 0xdc80 || r > 0xdcff {
				return "", ErrIllegalSequence
			}
			out.WriteByte(byte(r - 0xdc00))
			continue
		}
		if !utf8.ValidRune(r) {
			return "", ErrIllegalSequence
		}
		out.WriteRune(r)
	}
	return out.String(), ErrOK
}

func decodeWTF8Rune(raw []byte) (rune, int) {
	if len(raw) == 0 {
		return 0, 0
	}
	b0 := raw[0]
	if b0 < 0x80 {
		return rune(b0), 1
	}
	need := 0
	var r rune
	switch {
	case b0 >= 0xc2 && b0 <= 0xdf:
		need, r = 2, rune(b0&0x1f)
	case b0 >= 0xe0 && b0 <= 0xef:
		need, r = 3, rune(b0&0x0f)
	case b0 >= 0xf0 && b0 <= 0xf4:
		need, r = 4, rune(b0&0x07)
	default:
		return 0, 0
	}
	if len(raw) < need {
		return 0, 0
	}
	for i := 1; i < need; i++ {
		if raw[i]&0xc0 != 0x80 {
			return 0, 0
		}
		r = r<<6 | rune(raw[i]&0x3f)
	}
	if need == 3 {
		if b0 == 0xe0 && raw[1] < 0xa0 {
			return 0, 0
		}
		// Unlike UTF-8, WTF-8 intentionally permits surrogate code points.
	} else if need == 4 {
		if (b0 == 0xf0 && raw[1] < 0x90) || (b0 == 0xf4 && raw[1] > 0x8f) {
			return 0, 0
		}
	}
	return r, need
}

func readGuestTextMemory(m wago.HostModule, width textWidth, addressType wago.GuestMemoryAddressType, memoryIndex uint32, pointer, units uint64, wtf int32) (string, int32) {
	if !validWTF(wtf) {
		return "", ErrInvalid
	}
	if units > maxTextUnits {
		return "", ErrQuota
	}
	elementBytes, code := textElementBytes(width)
	if code != ErrOK {
		return "", code
	}
	byteLength, ok := checkedMul(units, elementBytes)
	if !ok {
		return "", ErrFault
	}
	var value string
	code = memoryRange(m, addressType, memoryIndex, pointer, byteLength, wago.GuestStorageRead, func(raw []byte) int32 {
		decoded, decodeCode := decodeTextBytes(raw, width, wtf)
		if decodeCode == ErrOK {
			value = decoded
		}
		return decodeCode
	})
	return value, code
}

func readGuestTextArray(m wago.HostModule, width textWidth, slot uint64, offset, units uint32, wtf int32) (string, int32) {
	if !validWTF(wtf) {
		return "", ErrInvalid
	}
	if uint64(units) > maxTextUnits {
		return "", ErrQuota
	}
	expected, code := textArrayStorage(width)
	if code != ErrOK {
		return "", code
	}
	elementBytes, code := textElementBytes(width)
	if code != ErrOK {
		return "", code
	}
	byteOffset, ok := checkedMul(uint64(offset), elementBytes)
	if !ok {
		return "", ErrRange
	}
	byteLength, ok := checkedMul(uint64(units), elementBytes)
	if !ok {
		return "", ErrRange
	}
	var value string
	code = arrayRange(m, slot, expected, byteOffset, byteLength, wago.GuestStorageRead, func(raw []byte) int32 {
		decoded, decodeCode := decodeTextBytes(raw, width, wtf)
		if decodeCode == ErrOK {
			value = decoded
		}
		return decodeCode
	})
	return value, code
}

func validateDNSName(value string) int32 {
	if value == "" || len(value) > 253 {
		return ErrInvalid
	}
	for i := 0; i < len(value); i++ {
		if value[i] == 0 || value[i] > 0x7f {
			return ErrInvalid
		}
	}
	return ErrOK
}
