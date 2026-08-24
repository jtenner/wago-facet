package facet

import (
	"errors"

	wago "github.com/wago-org/wago"
)

func memoryTextDestination(addressType wago.GuestMemoryAddressType, params []uint64, memoryIndexPos, pointerPos, capacityPos int) textMemoryDestination {
	dst := textMemoryDestination{
		addressType: addressType,
		memoryIndex: uint32(params[memoryIndexPos]),
		pointer:     params[pointerPos],
		capacity:    params[capacityPos],
	}
	if addressType == wago.GuestMemory32 {
		dst.pointer = uint64(uint32(dst.pointer))
		dst.capacity = uint64(uint32(dst.capacity))
	}
	return dst
}

func (p *Plugin) argsReadMemoryHost(width textWidth, addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 5 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: args_read memory host signature mismatch")})
		}
		wtf := int32(uint32(params[1]))
		if !validWTF(wtf) {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		state := p.stateFor(m)
		state.mu.Lock()
		index := uint32(params[0])
		if uint64(index) >= uint64(len(state.cfg.Args)) {
			state.mu.Unlock()
			results[1] = uint64(uint32(ErrRange))
			return
		}
		value := state.cfg.Args[index]
		state.mu.Unlock()
		units, code := copyTextToMemory(m, value, width, wtf, memoryTextDestination(addressType, params, 2, 3, 4))
		results[0] = units
		results[1] = uint64(uint32(code))
	}
}

func (p *Plugin) argsReadArrayHost(width textWidth) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 5 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: args_read_into_array host signature mismatch")})
		}
		wtf := int32(uint32(params[1]))
		if !validWTF(wtf) {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		state := p.stateFor(m)
		state.mu.Lock()
		index := uint32(params[0])
		if uint64(index) >= uint64(len(state.cfg.Args)) {
			state.mu.Unlock()
			results[1] = uint64(uint32(ErrRange))
			return
		}
		value := state.cfg.Args[index]
		state.mu.Unlock()
		units, code := copyTextToArray(m, value, width, wtf, params[2], uint32(params[3]), uint32(params[4]))
		results[0] = units
		results[1] = uint64(uint32(code))
	}
}

func (p *Plugin) envReadMemoryHost(width textWidth, addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 6 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: env_read memory host signature mismatch")})
		}
		field := int32(uint32(params[1]))
		if field != EnvName && field != EnvValue {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		wtf := int32(uint32(params[2]))
		if !validWTF(wtf) {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		state := p.stateFor(m)
		state.mu.Lock()
		index := uint32(params[0])
		if uint64(index) >= uint64(len(state.cfg.Env)) {
			state.mu.Unlock()
			results[1] = uint64(uint32(ErrRange))
			return
		}
		value, code := environmentField(state.cfg.Env[index], field)
		state.mu.Unlock()
		if code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		units, code := copyTextToMemory(m, value, width, wtf, memoryTextDestination(addressType, params, 3, 4, 5))
		results[0] = units
		results[1] = uint64(uint32(code))
	}
}

func (p *Plugin) envReadArrayHost(width textWidth) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 6 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: env_read_into_array host signature mismatch")})
		}
		field := int32(uint32(params[1]))
		if field != EnvName && field != EnvValue {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		wtf := int32(uint32(params[2]))
		if !validWTF(wtf) {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		state := p.stateFor(m)
		state.mu.Lock()
		index := uint32(params[0])
		if uint64(index) >= uint64(len(state.cfg.Env)) {
			state.mu.Unlock()
			results[1] = uint64(uint32(ErrRange))
			return
		}
		value, code := environmentField(state.cfg.Env[index], field)
		state.mu.Unlock()
		if code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		units, code := copyTextToArray(m, value, width, wtf, params[3], uint32(params[4]), uint32(params[5]))
		results[0] = units
		results[1] = uint64(uint32(code))
	}
}

func (p *Plugin) preopenNameReadMemoryHost(width textWidth, addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 5 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: fs_preopen_name_read memory host signature mismatch")})
		}
		wtf := int32(uint32(params[1]))
		if !validWTF(wtf) {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		state := p.stateFor(m)
		state.mu.Lock()
		index := uint32(params[0])
		if uint64(index) >= uint64(len(state.cfg.Preopens)) {
			state.mu.Unlock()
			results[1] = uint64(uint32(ErrRange))
			return
		}
		value := state.cfg.Preopens[index].Guest
		state.mu.Unlock()
		units, code := copyTextToMemory(m, value, width, wtf, memoryTextDestination(addressType, params, 2, 3, 4))
		results[0] = units
		results[1] = uint64(uint32(code))
	}
}

func (p *Plugin) preopenNameReadArrayHost(width textWidth) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 5 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: fs_preopen_name_read_into_array host signature mismatch")})
		}
		wtf := int32(uint32(params[1]))
		if !validWTF(wtf) {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		state := p.stateFor(m)
		state.mu.Lock()
		index := uint32(params[0])
		if uint64(index) >= uint64(len(state.cfg.Preopens)) {
			state.mu.Unlock()
			results[1] = uint64(uint32(ErrRange))
			return
		}
		value := state.cfg.Preopens[index].Guest
		state.mu.Unlock()
		units, code := copyTextToArray(m, value, width, wtf, params[2], uint32(params[3]), uint32(params[4]))
		results[0] = units
		results[1] = uint64(uint32(code))
	}
}

func (p *Plugin) dirIterNextReadMemoryHost(width textWidth, addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 5 || len(results) < 5 {
			panic(wago.HostTrap{Err: errors.New("facet: dir_iter_next memory host signature mismatch")})
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
		units, code := copyTextToMemory(m, snap.name, width, wtf, memoryTextDestination(addressType, params, 2, 3, 4))
		if code != ErrOK {
			results[4] = uint64(uint32(code))
			return
		}
		iter.index++
		iter.pending = nil
		results[0] = units
		results[1] = uint64(uint32(snap.kind))
		results[2] = snap.inode
		results[4] = uint64(uint32(ErrOK))
	}
}

func (p *Plugin) dirIterNextReadArrayHost(width textWidth) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 5 || len(results) < 5 {
			panic(wago.HostTrap{Err: errors.New("facet: dir_iter_next_into_array host signature mismatch")})
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
		units, code := copyTextToArray(m, snap.name, width, wtf, params[2], uint32(params[3]), uint32(params[4]))
		if code != ErrOK {
			results[4] = uint64(uint32(code))
			return
		}
		iter.index++
		iter.pending = nil
		results[0] = units
		results[1] = uint64(uint32(snap.kind))
		results[2] = snap.inode
		results[4] = uint64(uint32(ErrOK))
	}
}
