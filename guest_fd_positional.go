package facet

import (
	"errors"
	"math"

	wago "github.com/wago-org/wago"
	"golang.org/x/sys/unix"
)

func validatePositionalFD(h *handleEntry, op fdIOOperation) int32 {
	if h == nil || !h.isFD() {
		return ErrBadHandle
	}
	right := uint64(RightRead)
	if op == fdIOWrite {
		right = RightWrite
	}
	if h.rights&right == 0 {
		return ErrCapability
	}
	if h.file == nil {
		return ErrNotSupported
	}
	if h.file.directory {
		return ErrIsDirectory
	}
	return ErrOK
}

// Linux historically applies O_APPEND even to pwrite. Facet positional writes
// must honor the supplied offset, so temporarily suppress append on the open
// file description while the instance-state lock serializes operations on this
// handle. The original flags are restored before returning.
func pwriteExplicitOffset(h *handleEntry, buf []byte, offset int64) (int, error) {
	if h.flags&FDAppend == 0 {
		return unix.Pwrite(h.file.fd, buf, offset)
	}
	flags, err := unix.FcntlInt(uintptr(h.file.fd), unix.F_GETFL, 0)
	if err != nil {
		return 0, err
	}
	if flags&unix.O_APPEND == 0 {
		return unix.Pwrite(h.file.fd, buf, offset)
	}
	if _, err := unix.FcntlInt(uintptr(h.file.fd), unix.F_SETFL, flags&^unix.O_APPEND); err != nil {
		return 0, err
	}
	n, writeErr := unix.Pwrite(h.file.fd, buf, offset)
	_, restoreErr := unix.FcntlInt(uintptr(h.file.fd), unix.F_SETFL, flags)
	if restoreErr != nil {
		if n > 0 {
			// The write is already externally visible. Preserve Facet partial-I/O
			// semantics; the next descriptor operation can surface the host failure.
			return n, nil
		}
		return 0, restoreErr
	}
	return n, writeErr
}

func positionalFD(h *handleEntry, op fdIOOperation, offset int64, buf []byte) (uint64, int32) {
	if len(buf) == 0 {
		return 0, ErrOK
	}
	var n int
	var err error
	if op == fdIORead {
		n, err = unix.Pread(h.file.fd, buf, offset)
	} else {
		n, err = pwriteExplicitOffset(h, buf, offset)
	}
	return normalizeStreamResult(n, err)
}

func (p *Plugin) withPositionalFD(m wago.HostModule, raw uint64, op fdIOOperation, fn func(*handleEntry) (uint64, int32)) (uint64, int32) {
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	h, code := getFD(state, uint32(raw))
	if code != ErrOK {
		return 0, code
	}
	if code = validatePositionalFD(h, op); code != ErrOK {
		return 0, code
	}
	return fn(h)
}

func (p *Plugin) fdPositionalMemoryHost(op fdIOOperation, addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 5 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: positional fd memory signature mismatch")})
		}
		if params[1] > math.MaxInt64 {
			results[1] = uint64(uint32(ErrRange))
			return
		}
		pointer, length := params[3], params[4]
		if addressType == wago.GuestMemory32 {
			pointer, length = uint64(uint32(pointer)), uint64(uint32(length))
		}
		access := wago.GuestStorageRead
		if op == fdIORead {
			access = wago.GuestStorageWrite
		}
		transferred, code := p.withPositionalFD(m, params[0], op, func(h *handleEntry) (uint64, int32) {
			var n uint64
			var ioCode int32
			rangeCode := memoryRange(m, addressType, uint32(params[2]), pointer, length, access, func(buf []byte) int32 {
				n, ioCode = positionalFD(h, op, int64(params[1]), buf)
				return ioCode
			})
			if rangeCode != ErrOK {
				return 0, rangeCode
			}
			return n, ErrOK
		})
		results[0] = transferred
		results[1] = uint64(uint32(code))
	}
}

func (p *Plugin) fdPositionalArrayHost(op fdIOOperation, storage wago.GuestGCArrayStorage) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 5 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: positional fd array signature mismatch")})
		}
		if params[1] > math.MaxInt64 {
			results[1] = uint64(uint32(ErrRange))
			return
		}
		access := wago.GuestStorageRead
		if op == fdIORead {
			access = wago.GuestStorageWrite
		}
		transferred, code := p.withPositionalFD(m, params[0], op, func(h *handleEntry) (uint64, int32) {
			var n uint64
			var ioCode int32
			rangeCode := arrayRange(m, params[2], storage, params[3], params[4], access, func(buf []byte) int32 {
				n, ioCode = positionalFD(h, op, int64(params[1]), buf)
				return ioCode
			})
			if rangeCode != ErrOK {
				return 0, rangeCode
			}
			return n, ErrOK
		})
		results[0] = transferred
		results[1] = uint64(uint32(code))
	}
}

func (p *Plugin) positionalBindings() []binding {
	i32, i64, anyref := wago.ValI32, wago.ValI64, wago.ValAnyRef
	out := []binding{
		{"fd_pread_mem32", p.fdPositionalMemoryHost(fdIORead, wago.GuestMemory32), []wago.ValType{i32, i64, i32, i32, i32}, []wago.ValType{i64, i32}, CapFilesystemRead, "read file bytes at an explicit offset into Memory32"},
		{"fd_pread_mem64", p.fdPositionalMemoryHost(fdIORead, wago.GuestMemory64), []wago.ValType{i32, i64, i32, i64, i64}, []wago.ValType{i64, i32}, CapFilesystemRead, "read file bytes at an explicit offset into Memory64"},
		{"fd_pwrite_mem32", p.fdPositionalMemoryHost(fdIOWrite, wago.GuestMemory32), []wago.ValType{i32, i64, i32, i32, i32}, []wago.ValType{i64, i32}, CapFilesystemWrite, "write file bytes at an explicit offset from Memory32"},
		{"fd_pwrite_mem64", p.fdPositionalMemoryHost(fdIOWrite, wago.GuestMemory64), []wago.ValType{i32, i64, i32, i64, i64}, []wago.ValType{i64, i32}, CapFilesystemWrite, "write file bytes at an explicit offset from Memory64"},
	}
	for _, spec := range []struct {
		suffix  string
		storage wago.GuestGCArrayStorage
	}{{"i8", wago.GuestGCArrayI8}, {"i16", wago.GuestGCArrayI16}, {"i32", wago.GuestGCArrayI32}, {"i64", wago.GuestGCArrayI64}, {"v128", wago.GuestGCArrayV128}} {
		out = append(out,
			binding{"fd_pread_array_" + spec.suffix, p.fdPositionalArrayHost(fdIORead, spec.storage), []wago.ValType{i32, i64, anyref, i64, i64}, []wago.ValType{i64, i32}, CapFilesystemRead, "read file bytes at an explicit offset into a mutable GC array"},
			binding{"fd_pwrite_array_" + spec.suffix, p.fdPositionalArrayHost(fdIOWrite, spec.storage), []wago.ValType{i32, i64, anyref, i64, i64}, []wago.ValType{i64, i32}, CapFilesystemWrite, "write file bytes at an explicit offset from a GC array"},
		)
	}
	return out
}
