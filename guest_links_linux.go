package facet

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	wago "github.com/wago-org/wago"
	"golang.org/x/sys/unix"
)

func (p *Plugin) precheckDirectory(m wago.HostModule, raw uint64, required uint64) int32 {
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	_, code := getDirectoryCapability(state, raw, required)
	return code
}

func (p *Plugin) linkDecoded(m wago.HostModule, srcDir uint64, src string, dstDir uint64, dst string, flags uint32) int32 {
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	srcH, code := getDirectoryCapability(state, srcDir, RightPathLink)
	if code != ErrOK {
		return code
	}
	dstH, code := getDirectoryCapability(state, dstDir, RightPathLink)
	if code != ErrOK {
		return code
	}
	dstParent, dstLeaf, code := secureParent(dstH.file.fd, dst)
	if code != ErrOK {
		return code
	}
	defer unix.Close(dstParent)
	if flags&PathFollowSymlink != 0 {
		// Pin the followed source with openat2 before linking it. Using the procfs
		// fd path avoids a check-then-link race on the original source pathname.
		srcFD, code := openBeneath(srcH.file.fd, src, unix.O_PATH, 0)
		if code != ErrOK {
			return code
		}
		defer unix.Close(srcFD)
		procPath := "/proc/self/fd/" + strconv.Itoa(srcFD)
		if err := unix.Linkat(unix.AT_FDCWD, procPath, dstParent, dstLeaf, unix.AT_SYMLINK_FOLLOW); err != nil {
			if errors.Is(err, unix.ENOENT) && !strings.HasPrefix(procPath, "/proc/") {
				return ErrNotSupported
			}
			return pathCode(err)
		}
		return ErrOK
	}
	srcParent, srcLeaf, code := secureParent(srcH.file.fd, src)
	if code != ErrOK {
		return code
	}
	defer unix.Close(srcParent)
	return pathCode(unix.Linkat(srcParent, srcLeaf, dstParent, dstLeaf, 0))
}

func (p *Plugin) linkMemoryHost(width textWidth, addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 11 || len(results) < 1 {
			panic(wago.HostTrap{Err: errors.New("facet: path_link memory signature mismatch")})
		}
		flags := uint32(params[10])
		if flags&^PathFollowSymlink != 0 || !validWTF(int32(uint32(params[4]))) || !validWTF(int32(uint32(params[9]))) {
			results[0] = uint64(uint32(ErrInvalid))
			return
		}
		if code := p.precheckDirectory(m, params[0], RightPathLink); code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		if code := p.precheckDirectory(m, params[5], RightPathLink); code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		srcPtr, srcLen := params[2], params[3]
		dstPtr, dstLen := params[7], params[8]
		if addressType == wago.GuestMemory32 {
			srcPtr, srcLen = uint64(uint32(srcPtr)), uint64(uint32(srcLen))
			dstPtr, dstLen = uint64(uint32(dstPtr)), uint64(uint32(dstLen))
		}
		src, code := readGuestTextMemory(m, width, addressType, uint32(params[1]), srcPtr, srcLen, int32(uint32(params[4])))
		if code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		dst, code := readGuestTextMemory(m, width, addressType, uint32(params[6]), dstPtr, dstLen, int32(uint32(params[9])))
		if code == ErrOK {
			code = p.linkDecoded(m, params[0], src, params[5], dst, flags)
		}
		results[0] = uint64(uint32(code))
	}
}

func (p *Plugin) linkArrayHost(width textWidth) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 11 || len(results) < 1 {
			panic(wago.HostTrap{Err: errors.New("facet: path_link array signature mismatch")})
		}
		flags := uint32(params[10])
		if flags&^PathFollowSymlink != 0 || !validWTF(int32(uint32(params[4]))) || !validWTF(int32(uint32(params[9]))) {
			results[0] = uint64(uint32(ErrInvalid))
			return
		}
		if code := p.precheckDirectory(m, params[0], RightPathLink); code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		if code := p.precheckDirectory(m, params[5], RightPathLink); code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		src, code := readGuestTextArray(m, width, params[1], uint32(params[2]), uint32(params[3]), int32(uint32(params[4])))
		if code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		dst, code := readGuestTextArray(m, width, params[6], uint32(params[7]), uint32(params[8]), int32(uint32(params[9])))
		if code == ErrOK {
			code = p.linkDecoded(m, params[0], src, params[5], dst, flags)
		}
		results[0] = uint64(uint32(code))
	}
}

func validateSymlinkTarget(target string) int32 {
	if target == "" || strings.IndexByte(target, 0) >= 0 {
		return ErrInvalid
	}
	if strings.HasPrefix(target, "/") {
		return ErrPermission
	}
	return ErrOK
}

func (p *Plugin) symlinkDecoded(m wago.HostModule, target string, dstDir uint64, dst string) int32 {
	if code := validateSymlinkTarget(target); code != ErrOK {
		return code
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	dstH, code := getDirectoryCapability(state, dstDir, RightPathSymlink)
	if code != ErrOK {
		return code
	}
	parent, leaf, code := secureParent(dstH.file.fd, dst)
	if code != ErrOK {
		return code
	}
	defer unix.Close(parent)
	return pathCode(unix.Symlinkat(target, parent, leaf))
}

func (p *Plugin) symlinkMemoryHost(width textWidth, addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 9 || len(results) < 1 {
			panic(wago.HostTrap{Err: errors.New("facet: path_symlink memory signature mismatch")})
		}
		if !validWTF(int32(uint32(params[3]))) || !validWTF(int32(uint32(params[8]))) {
			results[0] = uint64(uint32(ErrInvalid))
			return
		}
		if code := p.precheckDirectory(m, params[4], RightPathSymlink); code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		targetPtr, targetLen := params[1], params[2]
		dstPtr, dstLen := params[6], params[7]
		if addressType == wago.GuestMemory32 {
			targetPtr, targetLen = uint64(uint32(targetPtr)), uint64(uint32(targetLen))
			dstPtr, dstLen = uint64(uint32(dstPtr)), uint64(uint32(dstLen))
		}
		target, code := readGuestTextMemory(m, width, addressType, uint32(params[0]), targetPtr, targetLen, int32(uint32(params[3])))
		if code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		dst, code := readGuestTextMemory(m, width, addressType, uint32(params[5]), dstPtr, dstLen, int32(uint32(params[8])))
		if code == ErrOK {
			code = p.symlinkDecoded(m, target, params[4], dst)
		}
		results[0] = uint64(uint32(code))
	}
}

func (p *Plugin) symlinkArrayHost(width textWidth) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 9 || len(results) < 1 {
			panic(wago.HostTrap{Err: errors.New("facet: path_symlink array signature mismatch")})
		}
		if !validWTF(int32(uint32(params[3]))) || !validWTF(int32(uint32(params[8]))) {
			results[0] = uint64(uint32(ErrInvalid))
			return
		}
		if code := p.precheckDirectory(m, params[4], RightPathSymlink); code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		target, code := readGuestTextArray(m, width, params[0], uint32(params[1]), uint32(params[2]), int32(uint32(params[3])))
		if code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		dst, code := readGuestTextArray(m, width, params[5], uint32(params[6]), uint32(params[7]), int32(uint32(params[8])))
		if code == ErrOK {
			code = p.symlinkDecoded(m, target, params[4], dst)
		}
		results[0] = uint64(uint32(code))
	}
}

func readlinkAt(parent int, leaf string) (string, int32) {
	size := 256
	for size <= 1<<20 {
		buf := make([]byte, size)
		n, err := unix.Readlinkat(parent, leaf, buf)
		if err != nil {
			return "", pathCode(err)
		}
		if n < len(buf) {
			value := string(buf[:n])
			if strings.HasPrefix(value, "/") {
				return "", ErrPermission
			}
			return value, ErrOK
		}
		size *= 2
	}
	return "", ErrNameTooLong
}

func (p *Plugin) readlinkDecoded(m wago.HostModule, directory uint64, value string) (string, int32) {
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	h, code := getDirectoryCapability(state, directory, RightPathReadlink)
	if code != ErrOK {
		return "", code
	}
	parent, leaf, code := secureParent(h.file.fd, value)
	if code != ErrOK {
		return "", code
	}
	defer unix.Close(parent)
	return readlinkAt(parent, leaf)
}

func readlinkEncode(value string, width textWidth, wtf int32) ([]byte, uint64, int32) {
	if strings.HasPrefix(value, "/") {
		return nil, 0, ErrPermission
	}
	return encodeText(value, width, wtf)
}

func (p *Plugin) readlinkLenMemoryHost(width textWidth, addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 6 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: path_readlink_len memory signature mismatch")})
		}
		if !validWTF(int32(uint32(params[4]))) || !validWTF(int32(uint32(params[5]))) {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		if code := p.precheckDirectory(m, params[0], RightPathReadlink); code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		ptr, units := params[2], params[3]
		if addressType == wago.GuestMemory32 {
			ptr, units = uint64(uint32(ptr)), uint64(uint32(units))
		}
		pathValue, code := readGuestTextMemory(m, width, addressType, uint32(params[1]), ptr, units, int32(uint32(params[4])))
		if code == ErrOK {
			var target string
			target, code = p.readlinkDecoded(m, params[0], pathValue)
			if code == ErrOK {
				_, results[0], code = readlinkEncode(target, width, int32(uint32(params[5])))
			}
		}
		results[1] = uint64(uint32(code))
		if code != ErrOK {
			results[0] = 0
		}
	}
}

func (p *Plugin) readlinkLenArrayHost(width textWidth) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 6 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: path_readlink_len array signature mismatch")})
		}
		if !validWTF(int32(uint32(params[4]))) || !validWTF(int32(uint32(params[5]))) {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		if code := p.precheckDirectory(m, params[0], RightPathReadlink); code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		pathValue, code := readGuestTextArray(m, width, params[1], uint32(params[2]), uint32(params[3]), int32(uint32(params[4])))
		if code == ErrOK {
			var target string
			target, code = p.readlinkDecoded(m, params[0], pathValue)
			if code == ErrOK {
				_, results[0], code = readlinkEncode(target, width, int32(uint32(params[5])))
			}
		}
		results[1] = uint64(uint32(code))
		if code != ErrOK {
			results[0] = 0
		}
	}
}

func validateTextMemoryDestination(m wago.HostModule, width textWidth, addressType wago.GuestMemoryAddressType, memory uint32, pointer, capacity uint64) int32 {
	elementBytes, code := textElementBytes(width)
	if code != ErrOK {
		return code
	}
	span, ok := checkedMul(capacity, elementBytes)
	if !ok {
		return ErrFault
	}
	return memoryRange(m, addressType, memory, pointer, span, wago.GuestStorageWrite, func([]byte) int32 { return ErrOK })
}

func validateTextArrayDestination(m wago.HostModule, width textWidth, slot uint64, offset, capacity uint32) int32 {
	elementBytes, code := textElementBytes(width)
	if code != ErrOK {
		return code
	}
	expected, code := textArrayStorage(width)
	if code != ErrOK {
		return code
	}
	byteOffset, ok := checkedMul(uint64(offset), elementBytes)
	if !ok {
		return ErrRange
	}
	byteLength, ok := checkedMul(uint64(capacity), elementBytes)
	if !ok {
		return ErrRange
	}
	return arrayRange(m, slot, expected, byteOffset, byteLength, wago.GuestStorageWrite, func([]byte) int32 { return ErrOK })
}

func (p *Plugin) readlinkMemoryHost(width textWidth, addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 9 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: path_readlink memory signature mismatch")})
		}
		if !validWTF(int32(uint32(params[4]))) || !validWTF(int32(uint32(params[8]))) {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		if code := p.precheckDirectory(m, params[0], RightPathReadlink); code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		pathPtr, pathLen := params[2], params[3]
		targetPtr, targetCap := params[6], params[7]
		if addressType == wago.GuestMemory32 {
			pathPtr, pathLen = uint64(uint32(pathPtr)), uint64(uint32(pathLen))
			targetPtr, targetCap = uint64(uint32(targetPtr)), uint64(uint32(targetCap))
		}
		pathValue, code := readGuestTextMemory(m, width, addressType, uint32(params[1]), pathPtr, pathLen, int32(uint32(params[4])))
		if code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		if code = validateTextMemoryDestination(m, width, addressType, uint32(params[5]), targetPtr, targetCap); code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		target, code := p.readlinkDecoded(m, params[0], pathValue)
		if code == ErrOK {
			results[0], code = copyTextToMemory(m, target, width, int32(uint32(params[8])), textMemoryDestination{addressType: addressType, memoryIndex: uint32(params[5]), pointer: targetPtr, capacity: targetCap})
		}
		results[1] = uint64(uint32(code))
	}
}

func (p *Plugin) readlinkIntoArrayHost(width textWidth) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 9 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: path_readlink_into_array signature mismatch")})
		}
		if !validWTF(int32(uint32(params[4]))) || !validWTF(int32(uint32(params[8]))) {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		if code := p.precheckDirectory(m, params[0], RightPathReadlink); code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		pathValue, code := readGuestTextArray(m, width, params[1], uint32(params[2]), uint32(params[3]), int32(uint32(params[4])))
		if code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		if code = validateTextArrayDestination(m, width, params[5], uint32(params[6]), uint32(params[7])); code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		target, code := p.readlinkDecoded(m, params[0], pathValue)
		if code == ErrOK {
			results[0], code = copyTextToArray(m, target, width, int32(uint32(params[8])), params[5], uint32(params[6]), uint32(params[7]))
		}
		results[1] = uint64(uint32(code))
	}
}

func (p *Plugin) readlinkAllocatedHost(width textWidth) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 6 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: path_readlink_array signature mismatch")})
		}
		if !validWTF(int32(uint32(params[4]))) || !validWTF(int32(uint32(params[5]))) {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		if code := p.precheckDirectory(m, params[0], RightPathReadlink); code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		pathValue, code := readGuestTextArray(m, width, params[1], uint32(params[2]), uint32(params[3]), int32(uint32(params[4])))
		if code == ErrOK {
			var target string
			target, code = p.readlinkDecoded(m, params[0], pathValue)
			if code == ErrOK {
				results[0], code = allocateTextArray(m, target, width, int32(uint32(params[5])))
			}
		}
		results[1] = uint64(uint32(code))
	}
}

func (p *Plugin) linkBindings() []binding {
	i32, i64, anyref := wago.ValI32, wago.ValI64, wago.ValAnyRef
	out := make([]binding, 0, 36)
	for _, spec := range []struct {
		suffix string
		width  textWidth
	}{{"i8", textI8}, {"i16", textI16}, {"i32", textI32}} {
		out = append(out,
			binding{"path_link_mem32_" + spec.suffix, p.linkMemoryHost(spec.width, wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32, i32, i32, i32, i32, i32, i32, i32}, []wago.ValType{i32}, CapFilesystemWrite, "hard-link capability-beneath Memory32 paths"},
			binding{"path_link_mem64_" + spec.suffix, p.linkMemoryHost(spec.width, wago.GuestMemory64), []wago.ValType{i32, i32, i64, i64, i32, i32, i32, i64, i64, i32, i32}, []wago.ValType{i32}, CapFilesystemWrite, "hard-link capability-beneath Memory64 paths"},
			binding{"path_link_array_" + spec.suffix, p.linkArrayHost(spec.width), []wago.ValType{i32, anyref, i32, i32, i32, i32, anyref, i32, i32, i32, i32}, []wago.ValType{i32}, CapFilesystemWrite, "hard-link capability-beneath GC-array paths"},
			binding{"path_symlink_mem32_" + spec.suffix, p.symlinkMemoryHost(spec.width, wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32, i32, i32, i32, i32, i32}, []wago.ValType{i32}, CapFilesystemWrite, "create a relative symbolic link from Memory32 text"},
			binding{"path_symlink_mem64_" + spec.suffix, p.symlinkMemoryHost(spec.width, wago.GuestMemory64), []wago.ValType{i32, i64, i64, i32, i32, i32, i64, i64, i32}, []wago.ValType{i32}, CapFilesystemWrite, "create a relative symbolic link from Memory64 text"},
			binding{"path_symlink_array_" + spec.suffix, p.symlinkArrayHost(spec.width), []wago.ValType{anyref, i32, i32, i32, i32, anyref, i32, i32, i32}, []wago.ValType{i32}, CapFilesystemWrite, "create a relative symbolic link from GC-array text"},
			binding{"path_readlink_len_mem32_" + spec.suffix, p.readlinkLenMemoryHost(spec.width, wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32, i32, i32}, []wago.ValType{i64, i32}, CapFilesystemRead, "measure a symbolic-link target from a Memory32 path"},
			binding{"path_readlink_mem32_" + spec.suffix, p.readlinkMemoryHost(spec.width, wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32, i32, i32, i32, i32, i32}, []wago.ValType{i64, i32}, CapFilesystemRead, "read a symbolic-link target into Memory32"},
			binding{"path_readlink_len_mem64_" + spec.suffix, p.readlinkLenMemoryHost(spec.width, wago.GuestMemory64), []wago.ValType{i32, i32, i64, i64, i32, i32}, []wago.ValType{i64, i32}, CapFilesystemRead, "measure a symbolic-link target from a Memory64 path"},
			binding{"path_readlink_mem64_" + spec.suffix, p.readlinkMemoryHost(spec.width, wago.GuestMemory64), []wago.ValType{i32, i32, i64, i64, i32, i32, i64, i64, i32}, []wago.ValType{i64, i32}, CapFilesystemRead, "read a symbolic-link target into Memory64"},
			binding{"path_readlink_len_array_" + spec.suffix, p.readlinkLenArrayHost(spec.width), []wago.ValType{i32, anyref, i32, i32, i32, i32}, []wago.ValType{i64, i32}, CapFilesystemRead, "measure a symbolic-link target from a GC-array path"},
			binding{"path_readlink_into_array_" + spec.suffix, p.readlinkIntoArrayHost(spec.width), []wago.ValType{i32, anyref, i32, i32, i32, anyref, i32, i32, i32}, []wago.ValType{i64, i32}, CapFilesystemRead, "read a symbolic-link target into a GC array"},
		)
	}
	return out
}

func (p *Plugin) allocatingReadlinkBindings() []binding {
	i32, anyref := wago.ValI32, wago.ValAnyRef
	out := make([]binding, 0, 3)
	for _, spec := range []struct {
		suffix string
		width  textWidth
	}{{"i8", textI8}, {"i16", textI16}, {"i32", textI32}} {
		out = append(out, binding{"path_readlink_array_" + spec.suffix, p.readlinkAllocatedHost(spec.width), []wago.ValType{i32, anyref, i32, i32, i32, i32}, []wago.ValType{anyref, i32}, CapFilesystemRead, "read a symbolic-link target into a caller-selected exact GC array"})
	}
	return out
}

var _ = fmt.Sprintf
