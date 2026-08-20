package facet

import (
	"errors"
	"io"

	wago "github.com/wago-org/wago"
	"golang.org/x/sys/unix"
)

type fdIOOperation uint8

const (
	fdIORead fdIOOperation = iota
	fdIOWrite
)

func validateSequentialFD(h *handleEntry, op fdIOOperation) int32 {
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
	if h.pre != nil {
		return ErrIsDirectory
	}
	if h.sock != nil {
		if h.sock.stype != SockStream {
			return ErrProtocol
		}
		if h.sock.state != socketConnected {
			return ErrNotConnected
		}
	}
	return ErrOK
}

func normalizeStreamResult(n int, err error) (uint64, int32) {
	if n > 0 {
		return uint64(n), ErrOK
	}
	if errors.Is(err, io.EOF) {
		return 0, ErrOK
	}
	return 0, errorCode(err)
}

func readSequentialFD(h *handleEntry, buf []byte) (uint64, int32) {
	if len(buf) == 0 {
		return 0, ErrOK
	}
	switch {
	case h.stdio != nil && h.stdio.reader != nil:
		n, err := h.stdio.reader.Read(buf)
		return normalizeStreamResult(n, err)
	case h.sock != nil:
		n, err := unix.Read(h.sock.fd, buf)
		return normalizeStreamResult(n, err)
	default:
		return 0, ErrNotSupported
	}
}

func writeSequentialFD(h *handleEntry, buf []byte) (uint64, int32) {
	if len(buf) == 0 {
		return 0, ErrOK
	}
	switch {
	case h.stdio != nil && h.stdio.writer != nil:
		n, err := h.stdio.writer.Write(buf)
		return normalizeStreamResult(n, err)
	case h.sock != nil:
		n, err := unix.Write(h.sock.fd, buf)
		return normalizeStreamResult(n, err)
	default:
		return 0, ErrNotSupported
	}
}

func (p *Plugin) withSequentialFD(m wago.HostModule, raw uint64, op fdIOOperation, fn func(*handleEntry) (uint64, int32)) (uint64, int32) {
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	h, code := getFD(state, uint32(raw))
	if code != ErrOK {
		return 0, code
	}
	if code = validateSequentialFD(h, op); code != ErrOK {
		return 0, code
	}
	return fn(h)
}

func (p *Plugin) fdMemoryIOHost(op fdIOOperation, addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 4 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: sequential fd memory host signature mismatch")})
		}

		memoryIndex := uint32(params[1])
		pointer := params[2]
		length := params[3]
		if addressType == wago.GuestMemory32 {
			pointer = uint64(uint32(pointer))
			length = uint64(uint32(length))
		}

		access := wago.GuestStorageRead
		if op == fdIORead {
			access = wago.GuestStorageWrite
		}
		transferred, code := p.withSequentialFD(m, params[0], op, func(h *handleEntry) (uint64, int32) {
			var n uint64
			var ioCode int32
			rangeCode := memoryRange(m, addressType, memoryIndex, pointer, length, access, func(buf []byte) int32 {
				if op == fdIORead {
					n, ioCode = readSequentialFD(h, buf)
				} else {
					n, ioCode = writeSequentialFD(h, buf)
				}
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

func (p *Plugin) fdArrayIOHost(op fdIOOperation, storageClass wago.GuestGCArrayStorage) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 4 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: sequential fd array host signature mismatch")})
		}

		access := wago.GuestStorageRead
		if op == fdIORead {
			access = wago.GuestStorageWrite
		}
		transferred, code := p.withSequentialFD(m, params[0], op, func(h *handleEntry) (uint64, int32) {
			var n uint64
			var ioCode int32
			rangeCode := arrayRange(m, params[1], storageClass, params[2], params[3], access, func(buf []byte) int32 {
				if op == fdIORead {
					n, ioCode = readSequentialFD(h, buf)
				} else {
					n, ioCode = writeSequentialFD(h, buf)
				}
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

func (p *Plugin) fdIOBindings() []binding {
	i32 := wago.ValI32
	i64 := wago.ValI64
	anyref := wago.ValAnyRef
	out := []binding{
		{"fd_read_mem32", p.fdMemoryIOHost(fdIORead, wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32}, []wago.ValType{i64, i32}, CapFilesystemRead, "read sequential descriptor bytes into indexed Memory32"},
		{"fd_read_mem64", p.fdMemoryIOHost(fdIORead, wago.GuestMemory64), []wago.ValType{i32, i32, i64, i64}, []wago.ValType{i64, i32}, CapFilesystemRead, "read sequential descriptor bytes into indexed Memory64"},
		{"fd_write_mem32", p.fdMemoryIOHost(fdIOWrite, wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32}, []wago.ValType{i64, i32}, CapFilesystemWrite, "write sequential descriptor bytes from indexed Memory32"},
		{"fd_write_mem64", p.fdMemoryIOHost(fdIOWrite, wago.GuestMemory64), []wago.ValType{i32, i32, i64, i64}, []wago.ValType{i64, i32}, CapFilesystemWrite, "write sequential descriptor bytes from indexed Memory64"},
	}
	for _, spec := range []struct {
		suffix  string
		storage wago.GuestGCArrayStorage
	}{
		{"i8", wago.GuestGCArrayI8},
		{"i16", wago.GuestGCArrayI16},
		{"i32", wago.GuestGCArrayI32},
		{"i64", wago.GuestGCArrayI64},
		{"v128", wago.GuestGCArrayV128},
	} {
		out = append(out,
			binding{"fd_read_array_" + spec.suffix, p.fdArrayIOHost(fdIORead, spec.storage), []wago.ValType{i32, anyref, i64, i64}, []wago.ValType{i64, i32}, CapFilesystemRead, "read sequential descriptor bytes into a mutable GC array"},
			binding{"fd_write_array_" + spec.suffix, p.fdArrayIOHost(fdIOWrite, spec.storage), []wago.ValType{i32, anyref, i64, i64}, []wago.ValType{i64, i32}, CapFilesystemWrite, "write sequential descriptor bytes from a GC array"},
		)
	}
	return out
}
