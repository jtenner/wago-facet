package facet

import (
	"crypto/rand"
	"errors"

	wago "github.com/wago-org/wago"
)

func randomFill(buf []byte) int32 {
	if len(buf) == 0 {
		return ErrOK
	}
	if _, err := rand.Read(buf); err != nil {
		return errorCode(err)
	}
	return ErrOK
}

func (p *Plugin) randomFillMemoryHost(addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 3 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: random_fill memory host signature mismatch")})
		}
		memoryIndex := uint32(params[0])
		pointer := params[1]
		length := params[2]
		if addressType == wago.GuestMemory32 {
			pointer = uint64(uint32(pointer))
			length = uint64(uint32(length))
		}
		code := memoryRange(m, addressType, memoryIndex, pointer, length, wago.GuestStorageWrite, randomFill)
		if code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		results[0] = length
		results[1] = uint64(uint32(ErrOK))
	}
}

func (p *Plugin) randomFillArrayHost(storageClass wago.GuestGCArrayStorage) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 3 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: random_fill array host signature mismatch")})
		}
		length := params[2]
		code := arrayRange(m, params[0], storageClass, params[1], length, wago.GuestStorageWrite, randomFill)
		if code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		results[0] = length
		results[1] = uint64(uint32(ErrOK))
	}
}
