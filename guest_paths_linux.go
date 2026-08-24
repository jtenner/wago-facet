package facet

import (
	"errors"
	"path"
	"strings"

	wago "github.com/wago-org/wago"
	"golang.org/x/sys/unix"
)

type fileResource struct {
	fd        int
	directory bool
}

func (f *fileResource) close() error {
	if f == nil || f.fd < 0 {
		return nil
	}
	fd := f.fd
	f.fd = -1
	return unix.Close(fd)
}

func (f *fileResource) pollFD() (int, bool) {
	if f == nil || f.fd < 0 {
		return 0, false
	}
	return f.fd, true
}

func resolvePathCode(err error) int32 {
	if err == nil {
		return ErrOK
	}
	if errors.Is(err, unix.EXDEV) {
		return ErrPermission
	}
	if errors.Is(err, unix.ENOSYS) {
		return ErrNotSupported
	}
	return errorCode(err)
}

func facetPath(value string) (string, int32) {
	if strings.IndexByte(value, 0) >= 0 || value == "" {
		return "", ErrInvalid
	}
	if strings.HasPrefix(value, "/") {
		return "", ErrPermission
	}
	return value, ErrOK
}

func openBeneath(dirfd int, name string, flags int, mode uint32) (int, int32) {
	name, code := facetPath(name)
	if code != ErrOK {
		return -1, code
	}
	how := &unix.OpenHow{Flags: uint64(flags | unix.O_CLOEXEC), Mode: uint64(mode), Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS}
	fd, err := unix.Openat2(dirfd, name, how)
	if err != nil {
		return -1, resolvePathCode(err)
	}
	return fd, ErrOK
}

func secureParent(dirfd int, name string) (parent int, leaf string, code int32) {
	name, code = facetPath(name)
	if code != ErrOK {
		return -1, "", code
	}
	parentName, leaf := path.Split(name)
	if leaf == "" || leaf == "." || leaf == ".." {
		return -1, "", ErrInvalid
	}
	parentName = strings.TrimSuffix(parentName, "/")
	if parentName == "" {
		parentName = "."
	}
	fd, code := openBeneath(dirfd, parentName, unix.O_PATH|unix.O_DIRECTORY, 0)
	if code != ErrOK {
		return -1, "", code
	}
	return fd, leaf, ErrOK
}

func getDirectoryCapability(state *instanceState, raw uint64, required uint64) (*handleEntry, int32) {
	h, code := getFD(state, uint32(raw))
	if code != ErrOK {
		return nil, code
	}
	if h.file == nil || !h.file.directory || h.file.fd < 0 {
		return nil, ErrNotDirectory
	}
	if h.rights&required != required {
		return nil, ErrCapability
	}
	return h, ErrOK
}

const allFacetRights = RightRead | RightWrite | RightSeek | RightTell | RightStat | RightSetSize | RightSync |
	RightPathOpen | RightPathCreate | RightPathRemove | RightPathRename | RightPathLink | RightPathSymlink | RightPathReadlink | RightDirIterate

func openFlagsToUnix(flags uint32, requested uint64) (int, int32) {
	if flags&^(OpenCreate|OpenExclusive|OpenTruncate|OpenDirectory|OpenNoFollow|OpenAppend|OpenNonblock) != 0 {
		return 0, ErrInvalid
	}
	if flags&OpenExclusive != 0 && flags&OpenCreate == 0 {
		return 0, ErrInvalid
	}
	if flags&OpenDirectory != 0 && flags&(OpenCreate|OpenTruncate) != 0 {
		return 0, ErrInvalid
	}
	if flags&OpenTruncate != 0 && requested&RightWrite == 0 {
		return 0, ErrCapability
	}
	if flags&OpenAppend != 0 && requested&RightWrite == 0 {
		return 0, ErrCapability
	}
	access := unix.O_RDONLY
	if requested&RightWrite != 0 {
		if requested&RightRead != 0 {
			access = unix.O_RDWR
		} else {
			access = unix.O_WRONLY
		}
	}
	if flags&OpenCreate != 0 {
		access |= unix.O_CREAT
	}
	if flags&OpenExclusive != 0 {
		access |= unix.O_EXCL
	}
	if flags&OpenTruncate != 0 {
		access |= unix.O_TRUNC
	}
	if flags&OpenDirectory != 0 {
		access |= unix.O_DIRECTORY
	}
	if flags&OpenNoFollow != 0 {
		access |= unix.O_NOFOLLOW
	}
	if flags&OpenAppend != 0 {
		access |= unix.O_APPEND
	}
	if flags&OpenNonblock != 0 {
		access |= unix.O_NONBLOCK
	}
	return access, ErrOK
}

func openFlagsToFDFlags(flags uint32) uint32 {
	var out uint32
	if flags&OpenAppend != 0 {
		out |= FDAppend
	}
	if flags&OpenNonblock != 0 {
		out |= FDNonblock
	}
	return out
}

func (p *Plugin) precheckPathOpen(m wago.HostModule, directory uint64, flags uint32, requested uint64) int32 {
	if requested&^allFacetRights != 0 {
		return ErrInvalid
	}
	if _, code := openFlagsToUnix(flags, requested); code != ErrOK {
		return code
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	parent, code := getDirectoryCapability(state, directory, RightPathOpen)
	if code != ErrOK {
		return code
	}
	if requested&^parent.rights != 0 {
		return ErrCapability
	}
	if flags&OpenCreate != 0 && parent.rights&RightPathCreate == 0 {
		return ErrCapability
	}
	return ErrOK
}

func (p *Plugin) pathOpenDecoded(m wago.HostModule, directory uint64, value string, openFlags uint32, requested uint64, results []uint64) {
	zeroResults(results)
	unixFlags, code := openFlagsToUnix(openFlags, requested)
	if code != ErrOK {
		results[1] = uint64(uint32(code))
		return
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	parent, code := getDirectoryCapability(state, directory, RightPathOpen)
	if code != ErrOK {
		results[1] = uint64(uint32(code))
		return
	}
	if requested&^parent.rights != 0 || openFlags&OpenCreate != 0 && parent.rights&RightPathCreate == 0 {
		results[1] = uint64(uint32(ErrCapability))
		return
	}
	var mode uint32
	if openFlags&OpenCreate != 0 {
		mode = 0o666
	}
	fd, code := openBeneath(parent.file.fd, value, unixFlags, mode)
	if code != ErrOK {
		results[1] = uint64(uint32(code))
		return
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		_ = unix.Close(fd)
		results[1] = uint64(uint32(errorCode(err)))
		return
	}
	entry := &handleEntry{kind: handleFile, rights: requested, flags: openFlagsToFDFlags(openFlags), file: &fileResource{fd: fd, directory: st.Mode&unix.S_IFMT == unix.S_IFDIR}}
	id, code := state.alloc(entry)
	if code != ErrOK {
		_ = unix.Close(fd)
		results[1] = uint64(uint32(code))
		return
	}
	results[0] = uint64(id)
	results[1] = uint64(uint32(ErrOK))
}

func (p *Plugin) pathOpenMemoryHost(width textWidth, addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 7 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: path_open memory signature mismatch")})
		}
		wtf, flags, requested := int32(uint32(params[4])), uint32(params[5]), params[6]
		if !validWTF(wtf) {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		if code := p.precheckPathOpen(m, params[0], flags, requested); code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		ptr, units := params[2], params[3]
		if addressType == wago.GuestMemory32 {
			ptr, units = uint64(uint32(ptr)), uint64(uint32(units))
		}
		value, code := readGuestTextMemory(m, width, addressType, uint32(params[1]), ptr, units, wtf)
		if code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		p.pathOpenDecoded(m, params[0], value, flags, requested, results)
	}
}

func (p *Plugin) pathOpenArrayHost(width textWidth) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 7 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: path_open array signature mismatch")})
		}
		wtf, flags, requested := int32(uint32(params[4])), uint32(params[5]), params[6]
		if !validWTF(wtf) {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		if code := p.precheckPathOpen(m, params[0], flags, requested); code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		value, code := readGuestTextArray(m, width, params[1], uint32(params[2]), uint32(params[3]), wtf)
		if code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		p.pathOpenDecoded(m, params[0], value, flags, requested, results)
	}
}

func statType(mode uint32) int32 {
	switch mode & unix.S_IFMT {
	case unix.S_IFREG:
		return FileTypeRegular
	case unix.S_IFDIR:
		return FileTypeDirectory
	case unix.S_IFLNK:
		return FileTypeSymlink
	case unix.S_IFCHR:
		return FileTypeChar
	case unix.S_IFBLK:
		return FileTypeBlock
	case unix.S_IFSOCK:
		return FileTypeSocket
	case unix.S_IFIFO:
		return FileTypeFIFO
	default:
		return FileTypeUnknown
	}
}

func writeUnixStat(results []uint64, st *unix.Stat_t, code int32) {
	zeroResults(results)
	if len(results) < 10 {
		return
	}
	if code != ErrOK || st == nil {
		results[9] = uint64(uint32(code))
		return
	}
	results[0] = uint64(uint32(statType(st.Mode)))
	results[1] = uint64(StatHasATime | StatHasMTime | StatHasCTime)
	if st.Size > 0 {
		results[2] = uint64(st.Size)
	}
	results[3] = uint64(st.Atim.Sec)
	results[4] = uint64(uint32(st.Atim.Nsec))
	results[5] = uint64(st.Mtim.Sec)
	results[6] = uint64(uint32(st.Mtim.Nsec))
	results[7] = uint64(st.Ctim.Sec)
	results[8] = uint64(uint32(st.Ctim.Nsec))
	results[9] = uint64(uint32(ErrOK))
}

func (p *Plugin) pathStatDecoded(m wago.HostModule, directory uint64, value string, flags uint32, results []uint64) {
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	parent, code := getDirectoryCapability(state, directory, RightStat)
	if code != ErrOK {
		writeUnixStat(results, nil, code)
		return
	}
	openFlags := unix.O_PATH
	if flags&PathFollowSymlink == 0 {
		openFlags |= unix.O_NOFOLLOW
	}
	fd, code := openBeneath(parent.file.fd, value, openFlags, 0)
	if code != ErrOK {
		writeUnixStat(results, nil, code)
		return
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		writeUnixStat(results, nil, errorCode(err))
		return
	}
	writeUnixStat(results, &st, ErrOK)
}

func (p *Plugin) pathStatMemoryHost(width textWidth, addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 6 || len(results) < 10 {
			panic(wago.HostTrap{Err: errors.New("facet: path_stat memory signature mismatch")})
		}
		wtf, flags := int32(uint32(params[4])), uint32(params[5])
		if !validWTF(wtf) || flags&^PathFollowSymlink != 0 {
			results[9] = uint64(uint32(ErrInvalid))
			return
		}
		if code := p.precheckDirectory(m, params[0], RightStat); code != ErrOK {
			results[9] = uint64(uint32(code))
			return
		}
		ptr, units := params[2], params[3]
		if addressType == wago.GuestMemory32 {
			ptr, units = uint64(uint32(ptr)), uint64(uint32(units))
		}
		value, code := readGuestTextMemory(m, width, addressType, uint32(params[1]), ptr, units, wtf)
		if code != ErrOK {
			results[9] = uint64(uint32(code))
			return
		}
		p.pathStatDecoded(m, params[0], value, flags, results)
	}
}

func (p *Plugin) pathStatArrayHost(width textWidth) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 6 || len(results) < 10 {
			panic(wago.HostTrap{Err: errors.New("facet: path_stat array signature mismatch")})
		}
		wtf, flags := int32(uint32(params[4])), uint32(params[5])
		if !validWTF(wtf) || flags&^PathFollowSymlink != 0 {
			results[9] = uint64(uint32(ErrInvalid))
			return
		}
		if code := p.precheckDirectory(m, params[0], RightStat); code != ErrOK {
			results[9] = uint64(uint32(code))
			return
		}
		value, code := readGuestTextArray(m, width, params[1], uint32(params[2]), uint32(params[3]), wtf)
		if code != ErrOK {
			results[9] = uint64(uint32(code))
			return
		}
		p.pathStatDecoded(m, params[0], value, flags, results)
	}
}

func (p *Plugin) createDirDecoded(m wago.HostModule, directory uint64, value string) int32 {
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	h, code := getDirectoryCapability(state, directory, RightPathCreate)
	if code != ErrOK {
		return code
	}
	parent, leaf, code := secureParent(h.file.fd, value)
	if code != ErrOK {
		return code
	}
	defer unix.Close(parent)
	return errorCode(unix.Mkdirat(parent, leaf, 0o777))
}

func (p *Plugin) createDirMemoryHost(width textWidth, addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 5 || len(results) < 1 {
			panic(wago.HostTrap{Err: errors.New("facet: path_create_dir memory signature mismatch")})
		}
		wtf := int32(uint32(params[4]))
		if !validWTF(wtf) {
			results[0] = uint64(uint32(ErrInvalid))
			return
		}
		if code := p.precheckDirectory(m, params[0], RightPathCreate); code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		ptr, units := params[2], params[3]
		if addressType == wago.GuestMemory32 {
			ptr, units = uint64(uint32(ptr)), uint64(uint32(units))
		}
		value, code := readGuestTextMemory(m, width, addressType, uint32(params[1]), ptr, units, wtf)
		if code == ErrOK {
			code = p.createDirDecoded(m, params[0], value)
		}
		results[0] = uint64(uint32(code))
	}
}

func (p *Plugin) createDirArrayHost(width textWidth) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 5 || len(results) < 1 {
			panic(wago.HostTrap{Err: errors.New("facet: path_create_dir array signature mismatch")})
		}
		wtf := int32(uint32(params[4]))
		if !validWTF(wtf) {
			results[0] = uint64(uint32(ErrInvalid))
			return
		}
		if code := p.precheckDirectory(m, params[0], RightPathCreate); code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		value, code := readGuestTextArray(m, width, params[1], uint32(params[2]), uint32(params[3]), wtf)
		if code == ErrOK {
			code = p.createDirDecoded(m, params[0], value)
		}
		results[0] = uint64(uint32(code))
	}
}

func (p *Plugin) removeDecoded(m wago.HostModule, directory uint64, value string, flags uint32) int32 {
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	h, code := getDirectoryCapability(state, directory, RightPathRemove)
	if code != ErrOK {
		return code
	}
	parent, leaf, code := secureParent(h.file.fd, value)
	if code != ErrOK {
		return code
	}
	defer unix.Close(parent)
	unlinkFlags := 0
	if flags == RemoveDirectory {
		unlinkFlags = unix.AT_REMOVEDIR
	}
	return errorCode(unix.Unlinkat(parent, leaf, unlinkFlags))
}

func (p *Plugin) removeMemoryHost(width textWidth, addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 6 || len(results) < 1 {
			panic(wago.HostTrap{Err: errors.New("facet: path_remove memory signature mismatch")})
		}
		wtf, flags := int32(uint32(params[4])), uint32(params[5])
		if !validWTF(wtf) || flags != RemoveFile && flags != RemoveDirectory {
			results[0] = uint64(uint32(ErrInvalid))
			return
		}
		if code := p.precheckDirectory(m, params[0], RightPathRemove); code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		ptr, units := params[2], params[3]
		if addressType == wago.GuestMemory32 {
			ptr, units = uint64(uint32(ptr)), uint64(uint32(units))
		}
		value, code := readGuestTextMemory(m, width, addressType, uint32(params[1]), ptr, units, wtf)
		if code == ErrOK {
			code = p.removeDecoded(m, params[0], value, flags)
		}
		results[0] = uint64(uint32(code))
	}
}

func (p *Plugin) removeArrayHost(width textWidth) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 6 || len(results) < 1 {
			panic(wago.HostTrap{Err: errors.New("facet: path_remove array signature mismatch")})
		}
		wtf, flags := int32(uint32(params[4])), uint32(params[5])
		if !validWTF(wtf) || flags != RemoveFile && flags != RemoveDirectory {
			results[0] = uint64(uint32(ErrInvalid))
			return
		}
		if code := p.precheckDirectory(m, params[0], RightPathRemove); code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		value, code := readGuestTextArray(m, width, params[1], uint32(params[2]), uint32(params[3]), wtf)
		if code == ErrOK {
			code = p.removeDecoded(m, params[0], value, flags)
		}
		results[0] = uint64(uint32(code))
	}
}

func renameUnixFlags(flags uint32) (uint, int32) {
	switch flags {
	case 0, RenameReplace:
		return 0, ErrOK
	case RenameNoReplace:
		return unix.RENAME_NOREPLACE, ErrOK
	case RenameExchange:
		return unix.RENAME_EXCHANGE, ErrOK
	default:
		return 0, ErrInvalid
	}
}

func (p *Plugin) renameDecoded(m wago.HostModule, srcDir uint64, src string, dstDir uint64, dst string, flags uint32) int32 {
	renameFlags, code := renameUnixFlags(flags)
	if code != ErrOK {
		return code
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	srcH, code := getDirectoryCapability(state, srcDir, RightPathRename)
	if code != ErrOK {
		return code
	}
	dstH, code := getDirectoryCapability(state, dstDir, RightPathRename)
	if code != ErrOK {
		return code
	}
	srcParent, srcLeaf, code := secureParent(srcH.file.fd, src)
	if code != ErrOK {
		return code
	}
	defer unix.Close(srcParent)
	dstParent, dstLeaf, code := secureParent(dstH.file.fd, dst)
	if code != ErrOK {
		return code
	}
	defer unix.Close(dstParent)
	return errorCode(unix.Renameat2(srcParent, srcLeaf, dstParent, dstLeaf, renameFlags))
}

func (p *Plugin) renameMemoryHost(width textWidth, addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 11 || len(results) < 1 {
			panic(wago.HostTrap{Err: errors.New("facet: path_rename memory signature mismatch")})
		}
		srcWTF, dstWTF, flags := int32(uint32(params[4])), int32(uint32(params[9])), uint32(params[10])
		if !validWTF(srcWTF) || !validWTF(dstWTF) {
			results[0] = uint64(uint32(ErrInvalid))
			return
		}
		if _, code := renameUnixFlags(flags); code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		if code := p.precheckDirectory(m, params[0], RightPathRename); code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		if code := p.precheckDirectory(m, params[5], RightPathRename); code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		srcPtr, srcUnits := params[2], params[3]
		dstPtr, dstUnits := params[7], params[8]
		if addressType == wago.GuestMemory32 {
			srcPtr, srcUnits = uint64(uint32(srcPtr)), uint64(uint32(srcUnits))
			dstPtr, dstUnits = uint64(uint32(dstPtr)), uint64(uint32(dstUnits))
		}
		src, code := readGuestTextMemory(m, width, addressType, uint32(params[1]), srcPtr, srcUnits, srcWTF)
		if code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		dst, code := readGuestTextMemory(m, width, addressType, uint32(params[6]), dstPtr, dstUnits, dstWTF)
		if code == ErrOK {
			code = p.renameDecoded(m, params[0], src, params[5], dst, flags)
		}
		results[0] = uint64(uint32(code))
	}
}

func (p *Plugin) renameArrayHost(width textWidth) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 11 || len(results) < 1 {
			panic(wago.HostTrap{Err: errors.New("facet: path_rename array signature mismatch")})
		}
		srcWTF, dstWTF, flags := int32(uint32(params[4])), int32(uint32(params[9])), uint32(params[10])
		if !validWTF(srcWTF) || !validWTF(dstWTF) {
			results[0] = uint64(uint32(ErrInvalid))
			return
		}
		if _, code := renameUnixFlags(flags); code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		if code := p.precheckDirectory(m, params[0], RightPathRename); code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		if code := p.precheckDirectory(m, params[5], RightPathRename); code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		src, code := readGuestTextArray(m, width, params[1], uint32(params[2]), uint32(params[3]), srcWTF)
		if code != ErrOK {
			results[0] = uint64(uint32(code))
			return
		}
		dst, code := readGuestTextArray(m, width, params[6], uint32(params[7]), uint32(params[8]), dstWTF)
		if code == ErrOK {
			code = p.renameDecoded(m, params[0], src, params[5], dst, flags)
		}
		results[0] = uint64(uint32(code))
	}
}

func (p *Plugin) pathBindings() []binding {
	i32, i64, anyref := wago.ValI32, wago.ValI64, wago.ValAnyRef
	statResults := []wago.ValType{i32, i32, i64, i64, i32, i64, i32, i64, i32, i32}
	out := make([]binding, 0, 45)
	for _, spec := range []struct {
		suffix string
		width  textWidth
	}{{"i8", textI8}, {"i16", textI16}, {"i32", textI32}} {
		out = append(out,
			binding{"path_open_mem32_" + spec.suffix, p.pathOpenMemoryHost(spec.width, wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32, i32, i32, i64}, []wago.ValType{i32, i32}, CapFilesystemOpen, "open a path beneath a directory capability from Memory32"},
			binding{"path_open_mem64_" + spec.suffix, p.pathOpenMemoryHost(spec.width, wago.GuestMemory64), []wago.ValType{i32, i32, i64, i64, i32, i32, i64}, []wago.ValType{i32, i32}, CapFilesystemOpen, "open a path beneath a directory capability from Memory64"},
			binding{"path_open_array_" + spec.suffix, p.pathOpenArrayHost(spec.width), []wago.ValType{i32, anyref, i32, i32, i32, i32, i64}, []wago.ValType{i32, i32}, CapFilesystemOpen, "open a path beneath a directory capability from a GC text array"},
			binding{"path_stat_mem32_" + spec.suffix, p.pathStatMemoryHost(spec.width, wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32, i32, i32}, statResults, CapFilesystemRead, "stat a capability-beneath Memory32 path"},
			binding{"path_stat_mem64_" + spec.suffix, p.pathStatMemoryHost(spec.width, wago.GuestMemory64), []wago.ValType{i32, i32, i64, i64, i32, i32}, statResults, CapFilesystemRead, "stat a capability-beneath Memory64 path"},
			binding{"path_stat_array_" + spec.suffix, p.pathStatArrayHost(spec.width), []wago.ValType{i32, anyref, i32, i32, i32, i32}, statResults, CapFilesystemRead, "stat a capability-beneath GC-array path"},
			binding{"path_create_dir_mem32_" + spec.suffix, p.createDirMemoryHost(spec.width, wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32, i32}, []wago.ValType{i32}, CapFilesystemWrite, "create a directory beneath a capability from Memory32"},
			binding{"path_create_dir_mem64_" + spec.suffix, p.createDirMemoryHost(spec.width, wago.GuestMemory64), []wago.ValType{i32, i32, i64, i64, i32}, []wago.ValType{i32}, CapFilesystemWrite, "create a directory beneath a capability from Memory64"},
			binding{"path_create_dir_array_" + spec.suffix, p.createDirArrayHost(spec.width), []wago.ValType{i32, anyref, i32, i32, i32}, []wago.ValType{i32}, CapFilesystemWrite, "create a directory beneath a capability from a GC text array"},
			binding{"path_remove_mem32_" + spec.suffix, p.removeMemoryHost(spec.width, wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32, i32, i32}, []wago.ValType{i32}, CapFilesystemWrite, "remove a capability-beneath Memory32 path"},
			binding{"path_remove_mem64_" + spec.suffix, p.removeMemoryHost(spec.width, wago.GuestMemory64), []wago.ValType{i32, i32, i64, i64, i32, i32}, []wago.ValType{i32}, CapFilesystemWrite, "remove a capability-beneath Memory64 path"},
			binding{"path_remove_array_" + spec.suffix, p.removeArrayHost(spec.width), []wago.ValType{i32, anyref, i32, i32, i32, i32}, []wago.ValType{i32}, CapFilesystemWrite, "remove a capability-beneath GC-array path"},
			binding{"path_rename_mem32_" + spec.suffix, p.renameMemoryHost(spec.width, wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32, i32, i32, i32, i32, i32, i32, i32}, []wago.ValType{i32}, CapFilesystemWrite, "rename between capability-beneath Memory32 paths"},
			binding{"path_rename_mem64_" + spec.suffix, p.renameMemoryHost(spec.width, wago.GuestMemory64), []wago.ValType{i32, i32, i64, i64, i32, i32, i32, i64, i64, i32, i32}, []wago.ValType{i32}, CapFilesystemWrite, "rename between capability-beneath Memory64 paths"},
			binding{"path_rename_array_" + spec.suffix, p.renameArrayHost(spec.width), []wago.ValType{i32, anyref, i32, i32, i32, i32, anyref, i32, i32, i32, i32}, []wago.ValType{i32}, CapFilesystemWrite, "rename between capability-beneath GC-array paths"},
		)
	}
	return out
}
