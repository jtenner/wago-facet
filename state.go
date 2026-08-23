package facet

import (
	"fmt"
	"io"
	"os"
	"sync"

	wago "github.com/wago-org/wago"
)

type handleKind uint8

const (
	handleStdin handleKind = iota + 1
	handleStdout
	handleStderr
	handlePreopen
	handleFile
	handleIterator
	handleSocket
	handlePoll
	handleResolver
)

type handleEntry struct {
	kind   handleKind
	rights uint64
	flags  uint32
	stdio  *stdioResource
	pre    *Preopen
	file   *fileResource
	iter   *dirIterator
	sock   *socketResource
	poll   *pollSet
	dns    *dnsResolver
}

func (h *handleEntry) isFD() bool {
	if h == nil {
		return false
	}
	switch h.kind {
	case handleStdin, handleStdout, handleStderr, handlePreopen, handleFile, handleSocket:
		return true
	default:
		return false
	}
}

func (h *handleEntry) close() error {
	if h == nil {
		return nil
	}
	if h.sock != nil {
		return h.sock.close()
	}
	if h.file != nil {
		return h.file.close()
	}
	if h.iter != nil {
		return h.iter.close()
	}
	return nil
}

type stdioResource struct {
	reader         io.Reader
	writer         io.Writer
	file           *os.File
	readImmediate  bool
	writeImmediate bool
}

type instanceState struct {
	mu sync.Mutex

	cfg          Config
	preopenFDs   []int
	nextHandle   uint32
	handles      map[uint32]*handleEntry
	stdioIDs     [3]uint32
	preopenIDs   []uint32
}

func newInstanceState(cfg Config, preopenFDs []int) *instanceState {
	cfg = normalizeConfig(cfg)
	return &instanceState{
		cfg:        cfg,
		preopenFDs: append([]int(nil), preopenFDs...),
		nextHandle: 1,
		handles:    make(map[uint32]*handleEntry),
		preopenIDs: make([]uint32, len(cfg.Preopens)),
	}
}

func (s *instanceState) alloc(entry *handleEntry) (uint32, int32) {
	if entry == nil {
		return 0, ErrInvalid
	}
	if uint32(len(s.handles)) >= s.cfg.MaxHandles {
		return 0, ErrQuota
	}
	id := s.nextHandle
	if id == 0 {
		return 0, ErrQuota
	}
	s.nextHandle++
	if s.nextHandle == 0 {
		// Never reuse a numeric handle during one instance lifetime.
		s.nextHandle = 0
	}
	s.handles[id] = entry
	return id, ErrOK
}

func (s *instanceState) get(id uint32) (*handleEntry, int32) {
	if id == 0 {
		return nil, ErrBadHandle
	}
	h := s.handles[id]
	if h == nil {
		return nil, ErrBadHandle
	}
	return h, ErrOK
}

func (s *instanceState) closeHandle(id uint32) int32 {
	h, code := s.get(id)
	if code != ErrOK {
		return code
	}
	if h.isFD() {
		s.removeFDFromPollSets(id)
	}
	delete(s.handles, id)
	for i, current := range s.stdioIDs {
		if current == id {
			s.stdioIDs[i] = 0
		}
	}
	for i, current := range s.preopenIDs {
		if current == id {
			s.preopenIDs[i] = 0
		}
	}
	if err := h.close(); err != nil {
		return errorCode(err)
	}
	return ErrOK
}

func (s *instanceState) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, h := range s.handles {
		_ = h.close()
		delete(s.handles, id)
	}
	for i := range s.stdioIDs {
		s.stdioIDs[i] = 0
	}
	for i := range s.preopenIDs {
		s.preopenIDs[i] = 0
	}
}

func (s *instanceState) stdio(which int) (uint32, int32) {
	if which < 0 || which >= len(s.stdioIDs) {
		return 0, ErrInvalid
	}
	if id := s.stdioIDs[which]; id != 0 {
		if _, ok := s.handles[id]; ok {
			return id, ErrOK
		}
		s.stdioIDs[which] = 0
	}
	var entry handleEntry
	switch which {
	case 0:
		r := &stdioResource{reader: s.cfg.Stdin}
		if file, ok := s.cfg.Stdin.(*os.File); ok {
			r.file = file
		} else {
			r.readImmediate = true
		}
		entry = handleEntry{kind: handleStdin, rights: RightRead | RightStat, stdio: r}
	case 1:
		r := &stdioResource{writer: s.cfg.Stdout}
		if file, ok := s.cfg.Stdout.(*os.File); ok {
			r.file = file
		} else {
			r.writeImmediate = true
		}
		entry = handleEntry{kind: handleStdout, rights: RightWrite | RightStat, stdio: r}
	case 2:
		r := &stdioResource{writer: s.cfg.Stderr}
		if file, ok := s.cfg.Stderr.(*os.File); ok {
			r.file = file
		} else {
			r.writeImmediate = true
		}
		entry = handleEntry{kind: handleStderr, rights: RightWrite | RightStat, stdio: r}
	}
	id, code := s.alloc(&entry)
	if code == ErrOK {
		s.stdioIDs[which] = id
	}
	return id, code
}

func (s *instanceState) preopen(index uint32) (uint32, int32) {
	if uint64(index) >= uint64(len(s.cfg.Preopens)) {
		return 0, ErrRange
	}
	if id := s.preopenIDs[index]; id != 0 {
		if _, ok := s.handles[id]; ok {
			return id, ErrOK
		}
		s.preopenIDs[index] = 0
	}
	if uint64(index) >= uint64(len(s.preopenFDs)) || s.preopenFDs[index] < 0 {
		return 0, ErrOther
	}
	p := &s.cfg.Preopens[index]
	fd, err := duplicatePinnedPreopen(s.preopenFDs[index])
	if err != nil {
		return 0, errorCode(err)
	}
	entry := &handleEntry{
		kind:   handlePreopen,
		rights: p.Rights,
		pre:    p,
		file:   &fileResource{fd: fd, directory: true},
	}
	id, code := s.alloc(entry)
	if code != ErrOK {
		_ = entry.close()
		return 0, code
	}
	s.preopenIDs[index] = id
	return id, ErrOK
}

func (s *instanceState) removeFDFromPollSets(fd uint32) {
	for _, h := range s.handles {
		if h != nil && h.kind == handlePoll && h.poll != nil {
			delete(h.poll.regs, fd)
		}
	}
}

type stateStore struct {
	mu         sync.Mutex
	cfg        Config
	preopenFDs []int
	states     map[wago.InstanceIdentity]*instanceState
}

func newStateStore(cfg Config, preopenFDs []int) *stateStore {
	return &stateStore{
		cfg:        normalizeConfig(cfg),
		preopenFDs: append([]int(nil), preopenFDs...),
		states:     make(map[wago.InstanceIdentity]*instanceState),
	}
}

func (s *stateStore) get(id wago.InstanceIdentity) *instanceState {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.states[id]
	if state == nil {
		state = newInstanceState(s.cfg, s.preopenFDs)
		s.states[id] = state
	}
	return state
}

func (s *stateStore) remove(id wago.InstanceIdentity) {
	s.mu.Lock()
	state := s.states[id]
	delete(s.states, id)
	s.mu.Unlock()
	if state != nil {
		state.closeAll()
	}
}

func (s *stateStore) closeAll() {
	s.mu.Lock()
	states := make([]*instanceState, 0, len(s.states))
	for id, state := range s.states {
		states = append(states, state)
		delete(s.states, id)
	}
	s.mu.Unlock()
	for _, state := range states {
		state.closeAll()
	}
}

func (s *instanceState) debugHandle(id uint32) string {
	h := s.handles[id]
	if h == nil {
		return "<invalid>"
	}
	return fmt.Sprintf("kind=%d rights=%#x flags=%#x", h.kind, h.rights, h.flags)
}
