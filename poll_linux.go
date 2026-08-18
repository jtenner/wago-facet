package facet

import (
	"errors"
	"math"
	"syscall"
	"time"

	wago "github.com/wago-org/wago"
	"golang.org/x/sys/unix"
)

type pollRegistration struct {
	events   uint32
	userdata uint64
}

type pollTimer struct {
	deadline uint64
	userdata uint64
}

type pollEvent struct {
	kind     int32
	source   uint32
	events   uint32
	userdata uint64
}

type pollSet struct {
	regs      map[uint32]pollRegistration
	timers    map[uint32]pollTimer
	nextTimer uint32
	ready     []pollEvent
}

func newPollSet() *pollSet {
	return &pollSet{regs: make(map[uint32]pollRegistration), timers: make(map[uint32]pollTimer), nextTimer: 1}
}

func (p *Plugin) pollCreateHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 0 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: poll_create host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	id, code := state.alloc(&handleEntry{kind: handlePoll, poll: newPollSet()})
	results[0] = uint64(id)
	results[1] = uint64(uint32(code))
}

func getPoll(state *instanceState, id uint32) (*pollSet, int32) {
	h, code := state.get(id)
	if code != ErrOK || h.kind != handlePoll || h.poll == nil {
		return nil, ErrBadHandle
	}
	return h.poll, ErrOK
}

func validPollInterest(events uint32) bool {
	return events != 0 && events&^(PollReadable|PollWritable) == 0
}

func (p *Plugin) pollAddFDHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 4 || len(results) < 1 {
		panic(wago.HostTrap{Err: errors.New("facet: poll_add_fd host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	poll, code := getPoll(state, uint32(params[0]))
	if code != ErrOK {
		results[0] = uint64(uint32(code))
		return
	}
	fd := uint32(params[1])
	h, code := state.get(fd)
	if code != ErrOK || !h.isFD() {
		results[0] = uint64(uint32(ErrBadHandle))
		return
	}
	events := uint32(params[2])
	if !validPollInterest(events) {
		results[0] = uint64(uint32(ErrInvalid))
		return
	}
	if _, exists := poll.regs[fd]; exists {
		results[0] = uint64(uint32(ErrExists))
		return
	}
	poll.regs[fd] = pollRegistration{events: events, userdata: params[3]}
	results[0] = uint64(uint32(ErrOK))
}

func (p *Plugin) pollUpdateFDHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 4 || len(results) < 1 {
		panic(wago.HostTrap{Err: errors.New("facet: poll_update_fd host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	poll, code := getPoll(state, uint32(params[0]))
	if code != ErrOK {
		results[0] = uint64(uint32(code))
		return
	}
	fd := uint32(params[1])
	if _, exists := poll.regs[fd]; !exists {
		results[0] = uint64(uint32(ErrNoEntry))
		return
	}
	if h, code := state.get(fd); code != ErrOK || !h.isFD() {
		results[0] = uint64(uint32(ErrBadHandle))
		return
	}
	events := uint32(params[2])
	if !validPollInterest(events) {
		results[0] = uint64(uint32(ErrInvalid))
		return
	}
	poll.regs[fd] = pollRegistration{events: events, userdata: params[3]}
	results[0] = uint64(uint32(ErrOK))
}

func (p *Plugin) pollRemoveFDHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 2 || len(results) < 1 {
		panic(wago.HostTrap{Err: errors.New("facet: poll_remove_fd host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	poll, code := getPoll(state, uint32(params[0]))
	if code != ErrOK {
		results[0] = uint64(uint32(code))
		return
	}
	fd := uint32(params[1])
	if _, exists := poll.regs[fd]; !exists {
		results[0] = uint64(uint32(ErrNoEntry))
		return
	}
	delete(poll.regs, fd)
	results[0] = uint64(uint32(ErrOK))
}

func (p *Plugin) pollAddTimerHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 3 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: poll_add_timer host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	poll, code := getPoll(state, uint32(params[0]))
	if code != ErrOK {
		results[1] = uint64(uint32(code))
		return
	}
	id := poll.nextTimer
	if id == 0 {
		results[1] = uint64(uint32(ErrQuota))
		return
	}
	poll.nextTimer++
	poll.timers[id] = pollTimer{deadline: params[1], userdata: params[2]}
	results[0] = uint64(id)
	results[1] = uint64(uint32(ErrOK))
}

func (p *Plugin) pollRemoveTimerHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 2 || len(results) < 1 {
		panic(wago.HostTrap{Err: errors.New("facet: poll_remove_timer host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	poll, code := getPoll(state, uint32(params[0]))
	if code != ErrOK {
		results[0] = uint64(uint32(code))
		return
	}
	id := uint32(params[1])
	if _, exists := poll.timers[id]; !exists {
		results[0] = uint64(uint32(ErrNoEntry))
		return
	}
	delete(poll.timers, id)
	results[0] = uint64(uint32(ErrOK))
}

type pollCandidate struct {
	handle uint32
	reg    pollRegistration
	index  int
}

func (p *Plugin) pollWaitHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 2 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: poll_wait host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	poll, code := getPoll(state, uint32(params[0]))
	if code != ErrOK {
		results[1] = uint64(uint32(code))
		return
	}
	if len(poll.ready) != 0 {
		results[1] = uint64(uint32(ErrBusy))
		return
	}
	deadline := params[1]
	if len(poll.regs) == 0 && len(poll.timers) == 0 && deadline == math.MaxUint64 {
		results[1] = uint64(uint32(ErrInvalid))
		return
	}

	for {
		now := p.monotonicNow()
		ready, pollfds, candidates := collectPollReady(state, poll, now)
		if len(ready) != 0 {
			poll.ready = ready
			removeFiredTimers(poll, ready)
			results[0] = uint64(len(ready))
			results[1] = uint64(uint32(ErrOK))
			return
		}
		if deadline != math.MaxUint64 && now >= deadline {
			results[1] = uint64(uint32(ErrOK))
			return
		}

		target := deadline
		for _, timer := range poll.timers {
			if target == math.MaxUint64 || timer.deadline < target {
				target = timer.deadline
			}
		}
		if len(pollfds) == 0 {
			if target == math.MaxUint64 {
				results[1] = uint64(uint32(ErrInvalid))
				return
			}
			sleepUntilMonotonic(p, target)
			continue
		}
		timeout := pollTimeoutMillis(now, target)
		count, err := unix.Poll(pollfds, timeout)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			results[1] = uint64(uint32(errorCode(err)))
			return
		}
		if count > 0 {
			ready = append(ready, eventsFromPollFDs(pollfds, candidates)...)
			now = p.monotonicNow()
			ready = append(ready, readyTimers(poll, now)...)
			if len(ready) != 0 {
				poll.ready = ready
				removeFiredTimers(poll, ready)
				results[0] = uint64(len(ready))
				results[1] = uint64(uint32(ErrOK))
				return
			}
		}
	}
}

func collectPollReady(state *instanceState, poll *pollSet, now uint64) ([]pollEvent, []unix.PollFd, []pollCandidate) {
	ready := readyTimers(poll, now)
	fds := make([]unix.PollFd, 0, len(poll.regs))
	candidates := make([]pollCandidate, 0, len(poll.regs))
	for id, reg := range poll.regs {
		h, code := state.get(id)
		if code != ErrOK || !h.isFD() {
			continue
		}
		if immediate, fd, events := pollDescriptor(h, reg.events); immediate != 0 {
			ready = append(ready, pollEvent{kind: PollSourceFD, source: id, events: immediate, userdata: reg.userdata})
		} else if fd >= 0 {
			idx := len(fds)
			fds = append(fds, unix.PollFd{Fd: int32(fd), Events: events})
			candidates = append(candidates, pollCandidate{handle: id, reg: reg, index: idx})
		}
	}
	if len(fds) != 0 {
		probe := append([]unix.PollFd(nil), fds...)
		if n, err := unix.Poll(probe, 0); err == nil && n > 0 {
			ready = append(ready, eventsFromPollFDs(probe, candidates)...)
		}
	}
	return ready, fds, candidates
}

func pollDescriptor(h *handleEntry, interest uint32) (immediate uint32, fd int, events int16) {
	fd = -1
	if h == nil {
		return PollError, -1, 0
	}
	if h.sock != nil {
		if raw, ok := h.sock.pollFD(); ok {
			fd = raw
		}
	} else if h.stdio != nil {
		if h.stdio.file != nil {
			fd = int(h.stdio.file.Fd())
		} else {
			if interest&PollReadable != 0 && h.stdio.readImmediate {
				immediate |= PollReadable
			}
			if interest&PollWritable != 0 && h.stdio.writeImmediate {
				immediate |= PollWritable
			}
			if immediate == 0 {
				immediate = PollError
			}
			return immediate, -1, 0
		}
	} else if h.pre != nil {
		return PollError, -1, 0
	}
	if interest&PollReadable != 0 {
		events |= unix.POLLIN
	}
	if interest&PollWritable != 0 {
		events |= unix.POLLOUT
	}
	return 0, fd, events
}

func eventsFromPollFDs(fds []unix.PollFd, candidates []pollCandidate) []pollEvent {
	ready := make([]pollEvent, 0)
	for _, candidate := range candidates {
		if candidate.index < 0 || candidate.index >= len(fds) {
			continue
		}
		re := fds[candidate.index].Revents
		if re == 0 {
			continue
		}
		var events uint32
		if re&unix.POLLIN != 0 {
			events |= PollReadable
		}
		if re&unix.POLLOUT != 0 {
			events |= PollWritable
		}
		if re&unix.POLLHUP != 0 {
			events |= PollHangup
		}
		if re&(unix.POLLERR|unix.POLLNVAL) != 0 {
			events |= PollError
		}
		if events != 0 {
			ready = append(ready, pollEvent{kind: PollSourceFD, source: candidate.handle, events: events, userdata: candidate.reg.userdata})
		}
	}
	return ready
}

func readyTimers(poll *pollSet, now uint64) []pollEvent {
	ready := make([]pollEvent, 0)
	for id, timer := range poll.timers {
		if timer.deadline <= now {
			ready = append(ready, pollEvent{kind: PollSourceTimer, source: id, events: PollTimer, userdata: timer.userdata})
		}
	}
	return ready
}

func removeFiredTimers(poll *pollSet, ready []pollEvent) {
	for _, event := range ready {
		if event.kind == PollSourceTimer {
			delete(poll.timers, event.source)
		}
	}
}

func pollTimeoutMillis(now, target uint64) int {
	if target == math.MaxUint64 {
		return -1
	}
	if target <= now {
		return 0
	}
	delta := target - now
	ms := (delta + uint64(time.Millisecond) - 1) / uint64(time.Millisecond)
	if ms > math.MaxInt32 {
		return math.MaxInt32
	}
	if ms == 0 {
		return 1
	}
	return int(ms)
}

func sleepUntilMonotonic(p *Plugin, target uint64) {
	now := p.monotonicNow()
	if target <= now {
		return
	}
	delta := target - now
	if delta > uint64(math.MaxInt64) {
		delta = uint64(math.MaxInt64)
	}
	time.Sleep(time.Duration(delta))
}

func (p *Plugin) pollNextHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 1 || len(results) < 6 {
		panic(wago.HostTrap{Err: errors.New("facet: poll_next host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	poll, code := getPoll(state, uint32(params[0]))
	if code != ErrOK {
		results[5] = uint64(uint32(code))
		return
	}
	if len(poll.ready) == 0 {
		results[4] = 1
		results[5] = uint64(uint32(ErrOK))
		return
	}
	event := poll.ready[0]
	copy(poll.ready, poll.ready[1:])
	poll.ready = poll.ready[:len(poll.ready)-1]
	results[0] = uint64(uint32(event.kind))
	results[1] = uint64(event.source)
	results[2] = uint64(event.events)
	results[3] = event.userdata
	results[4] = 0
	results[5] = uint64(uint32(ErrOK))
}
