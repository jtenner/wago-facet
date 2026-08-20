package facet

import (
	"errors"
	"fmt"
	"math"

	wago "github.com/wago-org/wago"
)

type textMemoryDestination struct {
	addressType wago.GuestMemoryAddressType
	memoryIndex uint32
	pointer     uint64
	capacity    uint64
}

func withGuestStorage(m wago.HostModule, fn func(wago.GuestStorage) int32) int32 {
	storageModule, ok := m.(wago.GuestStorageHostModule)
	if !ok {
		panic(wago.HostTrap{Err: errors.New("facet: Wago guest-storage API is unavailable")})
	}
	code := int32(ErrOther)
	if err := storageModule.WithGuestStorage(func(storage wago.GuestStorage) error {
		code = fn(storage)
		return nil
	}); err != nil {
		panic(wago.HostTrap{Err: fmt.Errorf("facet: borrow guest storage: %w", err)})
	}
	return code
}

func textElementBytes(width textWidth) (uint64, int32) {
	switch width {
	case textI8:
		return 1, ErrOK
	case textI16:
		return 2, ErrOK
	case textI32:
		return 4, ErrOK
	default:
		return 0, ErrInvalid
	}
}

func textArrayStorage(width textWidth) (wago.GuestGCArrayStorage, int32) {
	switch width {
	case textI8:
		return wago.GuestGCArrayI8, ErrOK
	case textI16:
		return wago.GuestGCArrayI16, ErrOK
	case textI32:
		return wago.GuestGCArrayI32, ErrOK
	default:
		return 0, ErrInvalid
	}
}

func checkedMul(a, b uint64) (uint64, bool) {
	if a != 0 && b > math.MaxUint64/a {
		return 0, false
	}
	return a * b, true
}

func checkedAdd(a, b uint64) (uint64, bool) {
	if a > math.MaxUint64-b {
		return 0, false
	}
	return a + b, true
}

func copyTextToMemory(m wago.HostModule, value string, width textWidth, wtf int32, dst textMemoryDestination) (uint64, int32) {
	encoded, units, textCode := encodeText(value, width, wtf)
	elementBytes, code := textElementBytes(width)
	if code != ErrOK {
		return 0, code
	}
	span, ok := checkedMul(dst.capacity, elementBytes)
	if !ok {
		return 0, ErrFault
	}

	code = withGuestStorage(m, func(storage wago.GuestStorage) int32 {
		info, err := storage.MemoryInfo(dst.memoryIndex)
		if err != nil {
			return ErrFault
		}
		if info.AddressType != dst.addressType {
			return ErrType
		}
		buf, err := storage.MemoryRange(dst.memoryIndex, dst.pointer, span, wago.GuestStorageWrite)
		if err != nil {
			return ErrFault
		}
		if textCode != ErrOK {
			return textCode
		}
		if units > dst.capacity {
			return ErrRange
		}
		copy(buf, encoded)
		return ErrOK
	})
	if code != ErrOK {
		return 0, code
	}
	return units, ErrOK
}

func copyTextToArray(m wago.HostModule, value string, width textWidth, wtf int32, slot uint64, offset, capacity uint32) (uint64, int32) {
	encoded, units, textCode := encodeText(value, width, wtf)
	elementBytes, code := textElementBytes(width)
	if code != ErrOK {
		return 0, code
	}
	expectedStorage, code := textArrayStorage(width)
	if code != ErrOK {
		return 0, code
	}

	code = withGuestStorage(m, func(storage wago.GuestStorage) int32 {
		ref, err := storage.GCRef(slot)
		if err != nil || ref.IsNull() {
			return ErrType
		}
		info, err := storage.GCArrayInfo(ref)
		if err != nil || info.Storage != expectedStorage || !info.Mutable {
			return ErrType
		}
		end, ok := checkedAdd(uint64(offset), uint64(capacity))
		if !ok || end > uint64(info.Length) {
			return ErrRange
		}
		payload, _, err := storage.GCArrayBytes(ref, wago.GuestStorageWrite)
		if err != nil {
			return ErrType
		}
		if textCode != ErrOK {
			return textCode
		}
		if units > uint64(capacity) {
			return ErrRange
		}
		start := uint64(offset) * elementBytes
		copy(payload[start:], encoded)
		return ErrOK
	})
	if code != ErrOK {
		return 0, code
	}
	return units, ErrOK
}

func memoryRange(m wago.HostModule, addressType wago.GuestMemoryAddressType, memoryIndex uint32, pointer, length uint64, access wago.GuestStorageAccess, fn func([]byte) int32) int32 {
	return withGuestStorage(m, func(storage wago.GuestStorage) int32 {
		info, err := storage.MemoryInfo(memoryIndex)
		if err != nil {
			return ErrFault
		}
		if info.AddressType != addressType {
			return ErrType
		}
		buf, err := storage.MemoryRange(memoryIndex, pointer, length, access)
		if err != nil {
			return ErrFault
		}
		return fn(buf)
	})
}

func arrayRange(m wago.HostModule, slot uint64, expectedStorage wago.GuestGCArrayStorage, byteOffset, byteLength uint64, access wago.GuestStorageAccess, fn func([]byte) int32) int32 {
	return withGuestStorage(m, func(storage wago.GuestStorage) int32 {
		ref, err := storage.GCRef(slot)
		if err != nil || ref.IsNull() {
			return ErrType
		}
		info, err := storage.GCArrayInfo(ref)
		if err != nil || info.Storage != expectedStorage {
			return ErrType
		}
		if access == wago.GuestStorageWrite && !info.Mutable {
			return ErrType
		}
		end, ok := checkedAdd(byteOffset, byteLength)
		if !ok {
			return ErrRange
		}
		payload, _, err := storage.GCArrayBytes(ref, access)
		if err != nil {
			return ErrType
		}
		if end > uint64(len(payload)) {
			return ErrRange
		}
		return fn(payload[byteOffset:end])
	})
}
