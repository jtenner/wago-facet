package facet

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	goruntime "runtime"
	"strings"
	"time"

	wago "github.com/wago-org/wago"
)

func (p *Plugin) abiVersionHost(_ wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 0 || len(results) < 1 {
		panic(wago.HostTrap{Err: errors.New("facet: abi_version host signature mismatch")})
	}
	results[0] = 1
}

func (p *Plugin) handleCloseHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 1 || len(results) < 1 {
		panic(wago.HostTrap{Err: errors.New("facet: handle_close host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	results[0] = uint64(uint32(state.closeHandle(uint32(params[0]))))
}

func (p *Plugin) procExitHost(_ wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 1 || len(results) != 0 {
		panic(wago.HostTrap{Err: errors.New("facet: proc_exit host signature mismatch")})
	}
	panic(wago.HostExit{Code: int32(uint32(params[0]))})
}

func (p *Plugin) procYieldHost(_ wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 0 || len(results) < 1 {
		panic(wago.HostTrap{Err: errors.New("facet: proc_yield host signature mismatch")})
	}
	goruntime.Gosched()
	results[0] = uint64(uint32(ErrOK))
}

func (p *Plugin) stdioHost(m wago.HostModule, which int, params, results []uint64) {
	zeroResults(results)
	if len(params) != 0 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: stdio host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	id, code := state.stdio(which)
	results[0] = uint64(id)
	results[1] = uint64(uint32(code))
}
func (p *Plugin) stdioStdinHost(m wago.HostModule, params, results []uint64) {
	p.stdioHost(m, 0, params, results)
}
func (p *Plugin) stdioStdoutHost(m wago.HostModule, params, results []uint64) {
	p.stdioHost(m, 1, params, results)
}
func (p *Plugin) stdioStderrHost(m wago.HostModule, params, results []uint64) {
	p.stdioHost(m, 2, params, results)
}

func (p *Plugin) argsCountHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 0 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: args_count host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	if uint64(len(state.cfg.Args)) > math.MaxUint32 {
		results[1] = uint64(uint32(ErrOverflow))
		return
	}
	results[0] = uint64(uint32(len(state.cfg.Args)))
	results[1] = uint64(uint32(ErrOK))
}

func (p *Plugin) argsLen(m wago.HostModule, params, results []uint64, width textWidth) {
	zeroResults(results)
	if len(params) != 2 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: args_len host signature mismatch")})
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
	if uint64(index) >= uint64(len(state.cfg.Args)) {
		results[1] = uint64(uint32(ErrRange))
		return
	}
	_, units, code := encodeText(state.cfg.Args[index], width, wtf)
	results[0] = units
	results[1] = uint64(uint32(code))
}
func (p *Plugin) argsLenI8Host(m wago.HostModule, params, results []uint64) {
	p.argsLen(m, params, results, textI8)
}
func (p *Plugin) argsLenI16Host(m wago.HostModule, params, results []uint64) {
	p.argsLen(m, params, results, textI16)
}
func (p *Plugin) argsLenI32Host(m wago.HostModule, params, results []uint64) {
	p.argsLen(m, params, results, textI32)
}

func (p *Plugin) envCountHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 0 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: env_count host signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	if uint64(len(state.cfg.Env)) > math.MaxUint32 {
		results[1] = uint64(uint32(ErrOverflow))
		return
	}
	results[0] = uint64(uint32(len(state.cfg.Env)))
	results[1] = uint64(uint32(ErrOK))
}

func environmentField(entry string, field int32) (string, int32) {
	at := strings.IndexByte(entry, '=')
	if at <= 0 {
		return "", ErrInvalid
	}
	switch field {
	case EnvName:
		return entry[:at], ErrOK
	case EnvValue:
		return entry[at+1:], ErrOK
	default:
		return "", ErrInvalid
	}
}

func (p *Plugin) envLen(m wago.HostModule, params, results []uint64, width textWidth) {
	zeroResults(results)
	if len(params) != 3 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: env_len host signature mismatch")})
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
	defer state.mu.Unlock()
	index := uint32(params[0])
	if uint64(index) >= uint64(len(state.cfg.Env)) {
		results[1] = uint64(uint32(ErrRange))
		return
	}
	value, code := environmentField(state.cfg.Env[index], field)
	if code != ErrOK {
		results[1] = uint64(uint32(code))
		return
	}
	_, units, code := encodeText(value, width, wtf)
	results[0] = units
	results[1] = uint64(uint32(code))
}
func (p *Plugin) envLenI8Host(m wago.HostModule, params, results []uint64) {
	p.envLen(m, params, results, textI8)
}
func (p *Plugin) envLenI16Host(m wago.HostModule, params, results []uint64) {
	p.envLen(m, params, results, textI16)
}
func (p *Plugin) envLenI32Host(m wago.HostModule, params, results []uint64) {
	p.envLen(m, params, results, textI32)
}

func (p *Plugin) clockSystemNowHost(_ wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 0 || len(results) < 3 {
		panic(wago.HostTrap{Err: errors.New("facet: clock_system_now host signature mismatch")})
	}
	now := time.Now()
	results[0] = uint64(now.Unix())
	results[1] = uint64(uint32(now.Nanosecond()))
	results[2] = uint64(uint32(ErrOK))
}

func (p *Plugin) clockMonotonicNowHost(_ wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 0 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: clock_monotonic_now host signature mismatch")})
	}
	results[0] = p.monotonicNow()
	results[1] = uint64(uint32(ErrOK))
}

func (p *Plugin) clockMonotonicResolutionHost(_ wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 0 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: clock_monotonic_resolution host signature mismatch")})
	}
	results[0] = 1
	results[1] = uint64(uint32(ErrOK))
}

func (p *Plugin) sleepForHost(_ wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 1 || len(results) < 1 {
		panic(wago.HostTrap{Err: errors.New("facet: sleep_for host signature mismatch")})
	}
	ns := params[0]
	if ns > uint64(math.MaxInt64) {
		results[0] = uint64(uint32(ErrOverflow))
		return
	}
	time.Sleep(time.Duration(ns))
	results[0] = uint64(uint32(ErrOK))
}

func (p *Plugin) sleepUntilHost(_ wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 1 || len(results) < 1 {
		panic(wago.HostTrap{Err: errors.New("facet: sleep_until host signature mismatch")})
	}
	deadline := params[0]
	now := p.monotonicNow()
	if deadline <= now {
		results[0] = uint64(uint32(ErrOK))
		return
	}
	delta := deadline - now
	if delta > uint64(math.MaxInt64) {
		results[0] = uint64(uint32(ErrOverflow))
		return
	}
	time.Sleep(time.Duration(delta))
	results[0] = uint64(uint32(ErrOK))
}

func (p *Plugin) randomU64Host(_ wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 0 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: random_u64 host signature mismatch")})
	}
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		results[1] = uint64(uint32(errorCode(err)))
		return
	}
	results[0] = binary.LittleEndian.Uint64(raw[:])
	results[1] = uint64(uint32(ErrOK))
}
