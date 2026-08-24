package facet

import (
	"errors"
	"io"
	"math"
	"os"

	wago "github.com/wago-org/wago"
	"golang.org/x/sys/unix"
)

type dirEntrySnapshot struct {
	name  string
	kind  int32
	inode uint64
}

type dirIterator struct {
	file    *os.File
	index   int
	pending *dirEntrySnapshot
}

func (d *dirIterator) close() error {
	if d == nil || d.file == nil {
		return nil
	}
	file := d.file
	d.file = nil
	return file.Close()
}

func (p *Plugin) preopenCountHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 0 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: fs_preopen_count host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	if uint64(len(state.cfg.Preopens)) > math.MaxUint32 {
		results[1] = uint64(uint32(ErrOverflow))
		return
	}
	results[0] = uint64(uint32(len(state.cfg.Preopens)))
	results[1] = uint64(uint32(ErrOK))
}

func (p *Plugin) preopenGetHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 1 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: fs_preopen_get host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	id, code := state.preopen(uint32(params[0]))
	results[0] = uint64(id)
	results[1] = uint64(uint32(code))
}

func (p *Plugin) preopenNameLen(m wago.HostModule, params, results []uint64, width textWidth) {
	zeroResults(results)
	if len(params) != 2 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: fs_preopen_name_len host signature mismatch")})
	}
	wtf := int32(uint32(params[1]))
	if !validWTF(wtf) {
		results[1] = uint64(uint32(ErrInvalid))
		return
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	index := uint32(params[0])
	if uint64(index) >= uint64(len(state.cfg.Preopens)) {
		results[1] = uint64(uint32(ErrRange))
		return
	}
	_, units, code := encodeText(state.cfg.Preopens[index].Guest, width, wtf)
	results[0] = units
	results[1] = uint64(uint32(code))
}
func (p *Plugin) preopenNameLenI8Host(m wago.HostModule, params, results []uint64) {
	p.preopenNameLen(m, params, results, textI8)
}
func (p *Plugin) preopenNameLenI16Host(m wago.HostModule, params, results []uint64) {
	p.preopenNameLen(m, params, results, textI16)
}
func (p *Plugin) preopenNameLenI32Host(m wago.HostModule, params, results []uint64) {
	p.preopenNameLen(m, params, results, textI32)
}

func getFD(state *instanceState, id uint32) (*handleEntry, int32) {
	h, code := state.get(id)
	if code != ErrOK || !h.isFD() {
		return nil, ErrBadHandle
	}
	return h, ErrOK
}

func (p *Plugin) fdRightsHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 1 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: fd_rights host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	h, code := getFD(state, uint32(params[0]))
	if code != ErrOK {
		results[1] = uint64(uint32(code))
		return
	}
	results[0] = h.rights
	results[1] = uint64(uint32(ErrOK))
}

func (p *Plugin) fdGetFlagsHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 1 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: fd_get_flags host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	h, code := getFD(state, uint32(params[0]))
	if code != ErrOK {
		results[1] = uint64(uint32(code))
		return
	}
	results[0] = uint64(h.flags)
	results[1] = uint64(uint32(ErrOK))
}

func (p *Plugin) fdSetFlagsHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 2 || len(results) < 1 {
		panic(wago.HostTrap{Err: errors.New("facet: fd_set_flags host signature mismatch")})
	}
	flags := uint32(params[1])
	if flags&^(FDAppend|FDNonblock) != 0 {
		results[0] = uint64(uint32(ErrInvalid))
		return
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	h, code := getFD(state, uint32(params[0]))
	if code != ErrOK {
		results[0] = uint64(uint32(code))
		return
	}
	if h.sock != nil {
		if flags&FDAppend != 0 {
			results[0] = uint64(uint32(ErrInvalid))
			return
		}
		if err := unix.SetNonblock(h.sock.fd, flags&FDNonblock != 0); err != nil {
			results[0] = uint64(uint32(errorCode(err)))
			return
		}
		h.sock.nonblock = flags&FDNonblock != 0
		h.flags = flags
		results[0] = uint64(uint32(ErrOK))
		return
	}
	if h.file != nil {
		if h.kind == handlePreopen || h.file.directory {
			if flags != 0 {
				results[0] = uint64(uint32(ErrInvalid))
				return
			}
			h.flags = 0
			results[0] = uint64(uint32(ErrOK))
			return
		}
		current, err := unix.FcntlInt(uintptr(h.file.fd), unix.F_GETFL, 0)
		if err != nil {
			results[0] = uint64(uint32(errorCode(err)))
			return
		}
		current &^= unix.O_APPEND | unix.O_NONBLOCK
		if flags&FDAppend != 0 {
			current |= unix.O_APPEND
		}
		if flags&FDNonblock != 0 {
			current |= unix.O_NONBLOCK
		}
		if _, err := unix.FcntlInt(uintptr(h.file.fd), unix.F_SETFL, current); err != nil {
			results[0] = uint64(uint32(errorCode(err)))
			return
		}
		h.flags = flags
		results[0] = uint64(uint32(ErrOK))
		return
	}
	if flags != 0 {
		results[0] = uint64(uint32(ErrNotSupported))
		return
	}
	h.flags = 0
	results[0] = uint64(uint32(ErrOK))
}

func (p *Plugin) fdStatHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 1 || len(results) < 10 {
		panic(wago.HostTrap{Err: errors.New("facet: fd_stat host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	h, code := getFD(state, uint32(params[0]))
	if code != ErrOK {
		results[9] = uint64(uint32(code))
		return
	}
	if h.rights&RightStat == 0 {
		results[9] = uint64(uint32(ErrCapability))
		return
	}
	if h.file != nil {
		var st unix.Stat_t
		if err := unix.Fstat(h.file.fd, &st); err != nil {
			writeUnixStat(results, nil, errorCode(err))
			return
		}
		writeUnixStat(results, &st, ErrOK)
		return
	}
	fileType, statFlags, size := int32(FileTypeUnknown), uint32(0), uint64(0)
	var msec int64
	var mnsec int32
	switch {
	case h.sock != nil:
		fileType = FileTypeSocket
	case h.stdio != nil:
		fileType = FileTypeChar
		if h.stdio.file != nil {
			if info, err := h.stdio.file.Stat(); err == nil {
				fileType = fileTypeFromInfo(info)
				if info.Size() > 0 {
					size = uint64(info.Size())
				}
				msec, mnsec = info.ModTime().Unix(), int32(info.ModTime().Nanosecond())
				statFlags |= StatHasMTime
			}
		}
	}
	results[0] = uint64(uint32(fileType))
	results[1] = uint64(statFlags)
	results[2] = size
	results[5] = uint64(msec)
	results[6] = uint64(uint32(mnsec))
	results[9] = uint64(uint32(ErrOK))
}

func fileTypeFromInfo(info os.FileInfo) int32 {
	if info == nil {
		return FileTypeUnknown
	}
	mode := info.Mode()
	switch {
	case mode.IsRegular():
		return FileTypeRegular
	case mode.IsDir():
		return FileTypeDirectory
	case mode&os.ModeSymlink != 0:
		return FileTypeSymlink
	case mode&os.ModeCharDevice != 0:
		return FileTypeChar
	case mode&os.ModeDevice != 0:
		return FileTypeBlock
	case mode&os.ModeNamedPipe != 0:
		return FileTypeFIFO
	default:
		return FileTypeUnknown
	}
}

func fileTypeFromUnixMode(mode uint32) int32 {
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

func seekWhence(whence int32) (int, bool) {
	switch whence {
	case SeekSet:
		return 0, true
	case SeekCur:
		return 1, true
	case SeekEnd:
		return 2, true
	default:
		return 0, false
	}
}

func (p *Plugin) fdSeekHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 3 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: fd_seek host signature mismatch")})
	}
	whence := int32(uint32(params[2]))
	osWhence, ok := seekWhence(whence)
	if !ok {
		results[1] = uint64(uint32(ErrInvalid))
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
	if h.rights&RightSeek == 0 {
		results[1] = uint64(uint32(ErrCapability))
		return
	}
	if h.file != nil {
		if h.file.directory {
			results[1] = uint64(uint32(ErrIsDirectory))
			return
		}
		offset, err := unix.Seek(h.file.fd, int64(params[1]), osWhence)
		if err != nil {
			results[1] = uint64(uint32(errorCode(err)))
			return
		}
		if offset < 0 {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		results[0] = uint64(offset)
		results[1] = uint64(uint32(ErrOK))
		return
	}
	results[1] = uint64(uint32(ErrPipe))
}

func (p *Plugin) fdTellHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 1 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: fd_tell host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	h, code := getFD(state, uint32(params[0]))
	if code != ErrOK {
		results[1] = uint64(uint32(code))
		return
	}
	if h.rights&RightTell == 0 {
		results[1] = uint64(uint32(ErrCapability))
		return
	}
	if h.file != nil {
		if h.file.directory {
			results[1] = uint64(uint32(ErrIsDirectory))
			return
		}
		offset, err := unix.Seek(h.file.fd, 0, 1)
		if err != nil {
			results[1] = uint64(uint32(errorCode(err)))
			return
		}
		if offset < 0 {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		results[0] = uint64(offset)
		results[1] = uint64(uint32(ErrOK))
		return
	}
	results[1] = uint64(uint32(ErrPipe))
}

func (p *Plugin) fdSetSizeHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 2 || len(results) < 1 {
		panic(wago.HostTrap{Err: errors.New("facet: fd_set_size host signature mismatch")})
	}
	if params[1] > math.MaxInt64 {
		results[0] = uint64(uint32(ErrFileTooLarge))
		return
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	h, code := getFD(state, uint32(params[0]))
	if code != ErrOK {
		results[0] = uint64(uint32(code))
		return
	}
	if h.rights&RightSetSize == 0 {
		results[0] = uint64(uint32(ErrCapability))
		return
	}
	if h.file != nil {
		if h.file.directory {
			results[0] = uint64(uint32(ErrIsDirectory))
			return
		}
		results[0] = uint64(uint32(errorCode(unix.Ftruncate(h.file.fd, int64(params[1])))))
		return
	}
	results[0] = uint64(uint32(ErrNotSupported))
}

func (p *Plugin) fdSyncHost(m wago.HostModule, params, results []uint64) {
	p.fdSync(m, params, results, false)
}
func (p *Plugin) fdDatasyncHost(m wago.HostModule, params, results []uint64) {
	p.fdSync(m, params, results, true)
}
func (p *Plugin) fdSync(m wago.HostModule, params, results []uint64, dataOnly bool) {
	zeroResults(results)
	if len(params) != 1 || len(results) < 1 {
		panic(wago.HostTrap{Err: errors.New("facet: fd_sync host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	h, code := getFD(state, uint32(params[0]))
	if code != ErrOK {
		results[0] = uint64(uint32(code))
		return
	}
	if h.rights&RightSync == 0 {
		results[0] = uint64(uint32(ErrCapability))
		return
	}
	if h.file != nil {
		var err error
		if dataOnly {
			err = unix.Fdatasync(h.file.fd)
		} else {
			err = unix.Fsync(h.file.fd)
		}
		results[0] = uint64(uint32(errorCode(err)))
		return
	}
	results[0] = uint64(uint32(ErrNotSupported))
}

func (p *Plugin) dirIterOpenHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 1 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: dir_iter_open host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	h, code := getFD(state, uint32(params[0]))
	if code != ErrOK {
		results[1] = uint64(uint32(code))
		return
	}
	if h.file == nil || !h.file.directory {
		results[1] = uint64(uint32(ErrNotDirectory))
		return
	}
	if h.rights&RightDirIterate == 0 {
		results[1] = uint64(uint32(ErrCapability))
		return
	}
	fd, err := unix.Openat(h.file.fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		results[1] = uint64(uint32(errorCode(err)))
		return
	}
	file := os.NewFile(uintptr(fd), "facet-dirfd")
	if file == nil {
		_ = unix.Close(fd)
		results[1] = uint64(uint32(ErrOther))
		return
	}
	iter := &dirIterator{file: file}
	id, code := state.alloc(&handleEntry{kind: handleIterator, iter: iter})
	if code != ErrOK {
		_ = iter.close()
	}
	results[0] = uint64(id)
	results[1] = uint64(uint32(code))
}

func getIterator(state *instanceState, id uint32) (*dirIterator, int32) {
	h, code := state.get(id)
	if code != ErrOK || h.kind != handleIterator || h.iter == nil || h.iter.file == nil {
		return nil, ErrBadHandle
	}
	return h.iter, ErrOK
}

func snapshotDirEntry(iter *dirIterator) (*dirEntrySnapshot, int32) {
	if iter.pending != nil {
		return iter.pending, ErrOK
	}
	if iter.file == nil {
		return nil, ErrBadHandle
	}
	entries, err := iter.file.ReadDir(1)
	if len(entries) == 0 {
		if err == nil || errors.Is(err, io.EOF) {
			return nil, ErrOK
		}
		return nil, errorCode(err)
	}
	entry := entries[0]
	snap := &dirEntrySnapshot{name: entry.Name(), kind: fileTypeFromDirEntryType(entry)}
	var st unix.Stat_t
	if err := unix.Fstatat(int(iter.file.Fd()), entry.Name(), &st, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		snap.inode = st.Ino
		snap.kind = fileTypeFromUnixMode(st.Mode)
	}
	iter.pending = snap
	return snap, ErrOK
}

func fileTypeFromDirEntryType(entry os.DirEntry) int32 {
	if entry == nil {
		return FileTypeUnknown
	}
	t := entry.Type()
	switch {
	case t.IsDir():
		return FileTypeDirectory
	case t&os.ModeSymlink != 0:
		return FileTypeSymlink
	case t.IsRegular():
		return FileTypeRegular
	case t&os.ModeCharDevice != 0:
		return FileTypeChar
	case t&os.ModeDevice != 0:
		return FileTypeBlock
	case t&os.ModeNamedPipe != 0:
		return FileTypeFIFO
	default:
		return FileTypeUnknown
	}
}

func (p *Plugin) dirIterNextLen(m wago.HostModule, params, results []uint64, width textWidth) {
	zeroResults(results)
	if len(params) != 2 || len(results) < 5 {
		panic(wago.HostTrap{Err: errors.New("facet: dir_iter_next_len host signature mismatch")})
	}
	wtf := int32(uint32(params[1]))
	if !validWTF(wtf) {
		results[4] = uint64(uint32(ErrInvalid))
		return
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	iter, code := getIterator(state, uint32(params[0]))
	if code != ErrOK {
		results[4] = uint64(uint32(code))
		return
	}
	snap, code := snapshotDirEntry(iter)
	if code != ErrOK {
		results[4] = uint64(uint32(code))
		return
	}
	if snap == nil {
		results[3] = 1
		results[4] = uint64(uint32(ErrOK))
		return
	}
	_, units, code := encodeText(snap.name, width, wtf)
	if code != ErrOK {
		results[4] = uint64(uint32(code))
		return
	}
	results[0] = units
	results[1] = uint64(uint32(snap.kind))
	results[2] = snap.inode
	results[3] = 0
	results[4] = uint64(uint32(ErrOK))
}
func (p *Plugin) dirIterNextLenI8Host(m wago.HostModule, params, results []uint64) {
	p.dirIterNextLen(m, params, results, textI8)
}
func (p *Plugin) dirIterNextLenI16Host(m wago.HostModule, params, results []uint64) {
	p.dirIterNextLen(m, params, results, textI16)
}
func (p *Plugin) dirIterNextLenI32Host(m wago.HostModule, params, results []uint64) {
	p.dirIterNextLen(m, params, results, textI32)
}

func (p *Plugin) dirIterRewindHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 1 || len(results) < 1 {
		panic(wago.HostTrap{Err: errors.New("facet: dir_iter_rewind host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	iter, code := getIterator(state, uint32(params[0]))
	if code != ErrOK {
		results[0] = uint64(uint32(code))
		return
	}
	if _, err := iter.file.Seek(0, io.SeekStart); err != nil {
		results[0] = uint64(uint32(errorCode(err)))
		return
	}
	iter.index = 0
	iter.pending = nil
	results[0] = uint64(uint32(ErrOK))
}
