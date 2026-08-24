package facet

import (
	"encoding/binary"
	"errors"
	"io"

	wago "github.com/wago-org/wago"
	"golang.org/x/sys/unix"
)

func vectoredFD(h *handleEntry, op fdIOOperation, buffers [][]byte) (uint64, int32) {
	if len(buffers) == 0 {
		return 0, ErrOK
	}
	switch {
	case h.file != nil:
		var n int
		var err error
		if op == fdIORead {
			n, err = unix.Readv(h.file.fd, buffers)
		} else {
			n, err = unix.Writev(h.file.fd, buffers)
		}
		return normalizeStreamResult(n, err)
	case h.sock != nil:
		var n int
		var err error
		if op == fdIORead {
			n, err = unix.Readv(h.sock.fd, buffers)
		} else {
			n, err = unix.Writev(h.sock.fd, buffers)
		}
		return normalizeStreamResult(n, err)
	case h.stdio != nil:
		total := 0
		for _, b := range buffers {
			total += len(b)
		}
		if total == 0 {
			return 0, ErrOK
		}
		tmp := make([]byte, total)
		if op == fdIORead {
			if h.stdio.reader == nil {
				return 0, ErrNotSupported
			}
			n, err := h.stdio.reader.Read(tmp)
			if n > 0 {
				remaining := tmp[:n]
				for _, b := range buffers {
					if len(remaining) == 0 {
						break
					}
					copied := copy(b, remaining)
					remaining = remaining[copied:]
				}
			}
			return normalizeStreamResult(n, err)
		}
		if h.stdio.writer == nil {
			return 0, ErrNotSupported
		}
		at := 0
		for _, b := range buffers {
			at += copy(tmp[at:], b)
		}
		n, err := h.stdio.writer.Write(tmp)
		return normalizeStreamResult(n, err)
	default:
		return 0, ErrNotSupported
	}
}

func validateVectoredFD(h *handleEntry, op fdIOOperation) int32 {
	return validateSequentialFD(h, op)
}

func addVectorBudget(total, length uint64) (uint64, int32) {
	next, ok := checkedAdd(total, length)
	if !ok || next > maxVectorBytes {
		return 0, ErrQuota
	}
	return next, ErrOK
}

func parseMemoryIOVecs(storage wago.GuestStorage, addressType wago.GuestMemoryAddressType, tableMemory uint32, tablePointer uint64, count uint32, access wago.GuestStorageAccess) ([][]byte, int32) {
	if count > maxIOVecs {
		return nil, ErrQuota
	}
	info, err := storage.MemoryInfo(tableMemory)
	if err != nil {
		return nil, ErrFault
	}
	if info.AddressType != addressType {
		return nil, ErrType
	}
	entrySize := uint64(16)
	if addressType == wago.GuestMemory64 {
		entrySize = 24
	}
	tableBytes, ok := checkedMul(uint64(count), entrySize)
	if !ok {
		return nil, ErrRange
	}
	table, err := storage.MemoryRange(tableMemory, tablePointer, tableBytes, wago.GuestStorageRead)
	if err != nil {
		return nil, ErrFault
	}
	buffers := make([][]byte, 0, count)
	var total uint64
	for i := uint32(0); i < count; i++ {
		off := uint64(i) * entrySize
		entry := table[off : off+entrySize]
		memoryIndex := binary.LittleEndian.Uint32(entry[0:4])
		var pointer, length uint64
		if addressType == wago.GuestMemory32 {
			if binary.LittleEndian.Uint32(entry[12:16]) != 0 {
				return nil, ErrInvalid
			}
			pointer = uint64(binary.LittleEndian.Uint32(entry[4:8]))
			length = uint64(binary.LittleEndian.Uint32(entry[8:12]))
		} else {
			if binary.LittleEndian.Uint32(entry[4:8]) != 0 {
				return nil, ErrInvalid
			}
			pointer = binary.LittleEndian.Uint64(entry[8:16])
			length = binary.LittleEndian.Uint64(entry[16:24])
		}
		var code int32
		total, code = addVectorBudget(total, length)
		if code != ErrOK {
			return nil, code
		}
		childInfo, err := storage.MemoryInfo(memoryIndex)
		if err != nil {
			return nil, ErrFault
		}
		if childInfo.AddressType != addressType {
			return nil, ErrType
		}
		buf, err := storage.MemoryRange(memoryIndex, pointer, length, access)
		if err != nil {
			return nil, ErrFault
		}
		buffers = append(buffers, buf)
	}
	return buffers, ErrOK
}

func parseGCIOVecs(storage wago.GuestStorage, slot uint64, first, count uint32, expected wago.GuestGCArrayStorage, access wago.GuestStorageAccess) ([][]byte, int32) {
	if count > maxIOVecs {
		return nil, ErrQuota
	}
	outer, err := storage.GCRef(slot)
	if err != nil || outer.IsNull() {
		return nil, ErrType
	}
	outerInfo, err := storage.GCArrayInfo(outer)
	if err != nil || outerInfo.Storage != wago.GuestGCArrayRef {
		return nil, ErrType
	}
	end, ok := checkedAdd(uint64(first), uint64(count))
	if !ok || end > uint64(outerInfo.Length) {
		return nil, ErrRange
	}
	buffers := make([][]byte, 0, count)
	var total uint64
	for i := uint32(0); i < count; i++ {
		child, err := storage.GCArrayRef(outer, first+i)
		if err != nil || child.IsNull() {
			return nil, ErrType
		}
		info, err := storage.GCArrayInfo(child)
		if err != nil || info.Storage != expected {
			return nil, ErrType
		}
		if access == wago.GuestStorageWrite && !info.Mutable {
			return nil, ErrType
		}
		payload, _, err := storage.GCArrayBytes(child, access)
		if err != nil {
			return nil, ErrType
		}
		var code int32
		total, code = addVectorBudget(total, uint64(len(payload)))
		if code != ErrOK {
			return nil, code
		}
		buffers = append(buffers, payload)
	}
	return buffers, ErrOK
}

func withStorageForIO(m wago.HostModule, fn func(wago.GuestStorage) (uint64, int32)) (uint64, int32) {
	storageModule, ok := m.(wago.GuestStorageHostModule)
	if !ok {
		panic(wago.HostTrap{Err: errors.New("facet: Wago guest-storage API is unavailable")})
	}
	var n uint64
	code := int32(ErrOther)
	if err := storageModule.WithGuestStorage(func(storage wago.GuestStorage) error {
		n, code = fn(storage)
		return nil
	}); err != nil {
		panic(wago.HostTrap{Err: err})
	}
	return n, code
}

func (p *Plugin) fdReadvMemoryHost(op fdIOOperation, addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 4 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: fd_readv/writev memory signature mismatch")})
		}
		pointer := params[2]
		if addressType == wago.GuestMemory32 {
			pointer = uint64(uint32(pointer))
		}
		count := uint32(params[3])
		if count > maxIOVecs {
			results[1] = uint64(uint32(ErrQuota))
			return
		}
		state := p.stateFor(m)
		state.mu.Lock()
		defer state.mu.Unlock()
		h, code := getFD(state, uint32(params[0]))
		if code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		if code = validateVectoredFD(h, op); code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		access := wago.GuestStorageRead
		if op == fdIORead {
			access = wago.GuestStorageWrite
		}
		n, code := withStorageForIO(m, func(storage wago.GuestStorage) (uint64, int32) {
			buffers, c := parseMemoryIOVecs(storage, addressType, uint32(params[1]), pointer, count, access)
			if c != ErrOK {
				return 0, c
			}
			return vectoredFD(h, op, buffers)
		})
		results[0] = n
		results[1] = uint64(uint32(code))
	}
}

func (p *Plugin) fdReadvArrayHost(op fdIOOperation, expected wago.GuestGCArrayStorage) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 4 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: fd_readv/writev array signature mismatch")})
		}
		count := uint32(params[3])
		if count > maxIOVecs {
			results[1] = uint64(uint32(ErrQuota))
			return
		}
		state := p.stateFor(m)
		state.mu.Lock()
		defer state.mu.Unlock()
		h, code := getFD(state, uint32(params[0]))
		if code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		if code = validateVectoredFD(h, op); code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		access := wago.GuestStorageRead
		if op == fdIORead {
			access = wago.GuestStorageWrite
		}
		n, code := withStorageForIO(m, func(storage wago.GuestStorage) (uint64, int32) {
			buffers, c := parseGCIOVecs(storage, params[1], uint32(params[2]), count, expected, access)
			if c != ErrOK {
				return 0, c
			}
			return vectoredFD(h, op, buffers)
		})
		results[0] = n
		results[1] = uint64(uint32(code))
	}
}

func (p *Plugin) vectoredBindings() []binding {
	i32, i64, anyref := wago.ValI32, wago.ValI64, wago.ValAnyRef
	out := []binding{
		{"fd_readv_mem32", p.fdReadvMemoryHost(fdIORead, wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32}, []wago.ValType{i64, i32}, CapFDRead, "read into a fully validated Memory32 iovec"},
		{"fd_writev_mem32", p.fdReadvMemoryHost(fdIOWrite, wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32}, []wago.ValType{i64, i32}, CapFDWrite, "write from a fully validated Memory32 iovec"},
		{"fd_readv_mem64", p.fdReadvMemoryHost(fdIORead, wago.GuestMemory64), []wago.ValType{i32, i32, i64, i32}, []wago.ValType{i64, i32}, CapFDRead, "read into a fully validated Memory64 iovec"},
		{"fd_writev_mem64", p.fdReadvMemoryHost(fdIOWrite, wago.GuestMemory64), []wago.ValType{i32, i32, i64, i32}, []wago.ValType{i64, i32}, CapFDWrite, "write from a fully validated Memory64 iovec"},
	}
	for _, spec := range []struct {
		suffix  string
		storage wago.GuestGCArrayStorage
	}{{"i8", wago.GuestGCArrayI8}, {"i16", wago.GuestGCArrayI16}, {"i32", wago.GuestGCArrayI32}, {"i64", wago.GuestGCArrayI64}, {"v128", wago.GuestGCArrayV128}} {
		out = append(out,
			binding{"fd_readv_array_" + spec.suffix, p.fdReadvArrayHost(fdIORead, spec.storage), []wago.ValType{i32, anyref, i32, i32}, []wago.ValType{i64, i32}, CapFDRead, "read into fully validated nested GC arrays"},
			binding{"fd_writev_array_" + spec.suffix, p.fdReadvArrayHost(fdIOWrite, spec.storage), []wago.ValType{i32, anyref, i32, i32}, []wago.ValType{i64, i32}, CapFDWrite, "write from fully validated nested GC arrays"},
		)
	}
	return out
}

var _ = io.EOF
