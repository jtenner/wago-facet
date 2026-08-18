package facet

import (
	"encoding/binary"
	"errors"
	"math"
	"syscall"

	wago "github.com/wago-org/wago"
	"golang.org/x/sys/unix"
)

type socketState uint8

const (
	socketOpen socketState = iota
	socketBound
	socketConnecting
	socketConnected
	socketListening
	socketFailed
)

type socketAddress struct {
	family int32
	hi     uint64
	lo     uint64
	port   uint16
	scope  uint32
}

type socketResource struct {
	fd         int
	family     int32
	stype      int32
	protocol   int32
	state      socketState
	nonblock   bool
	pending    socketAddress
	hasPending bool
}

func (s *socketResource) close() error {
	if s == nil || s.fd < 0 {
		return nil
	}
	fd := s.fd
	s.fd = -1
	return unix.Close(fd)
}

func (s *socketResource) pollFD() (int, bool) {
	if s == nil || s.fd < 0 {
		return 0, false
	}
	return s.fd, true
}

func socketDomain(family int32) (int, bool) {
	switch family {
	case AFInet4:
		return unix.AF_INET, true
	case AFInet6:
		return unix.AF_INET6, true
	default:
		return 0, false
	}
}

func socketTypeAndProtocol(stype, protocol int32) (int, int, int32) {
	switch stype {
	case SockStream:
		if protocol != ProtoDefault && protocol != ProtoTCP {
			return 0, 0, ErrProtocol
		}
		return unix.SOCK_STREAM, unix.IPPROTO_TCP, ErrOK
	case SockDgram:
		if protocol != ProtoDefault && protocol != ProtoUDP {
			return 0, 0, ErrProtocol
		}
		return unix.SOCK_DGRAM, unix.IPPROTO_UDP, ErrOK
	default:
		return 0, 0, ErrInvalid
	}
}

func addressFromFields(family int32, hi, lo uint64, port, scope uint32) (socketAddress, int32) {
	if port > math.MaxUint16 {
		return socketAddress{}, ErrAddressInvalid
	}
	a := socketAddress{family: family, hi: hi, lo: lo, port: uint16(port), scope: scope}
	switch family {
	case AFInet4:
		if hi != 0 || lo>>32 != 0 || scope != 0 {
			return socketAddress{}, ErrAddressInvalid
		}
	case AFInet6:
	default:
		return socketAddress{}, ErrAddressInvalid
	}
	return a, ErrOK
}

func (a socketAddress) sockaddr() (unix.Sockaddr, error) {
	switch a.family {
	case AFInet4:
		var raw [4]byte
		binary.BigEndian.PutUint32(raw[:], uint32(a.lo))
		return &unix.SockaddrInet4{Port: int(a.port), Addr: raw}, nil
	case AFInet6:
		var raw [16]byte
		binary.BigEndian.PutUint64(raw[:8], a.hi)
		binary.BigEndian.PutUint64(raw[8:], a.lo)
		return &unix.SockaddrInet6{Port: int(a.port), ZoneId: a.scope, Addr: raw}, nil
	default:
		return nil, syscall.EAFNOSUPPORT
	}
}

func addressFromSockaddr(sa unix.Sockaddr) (socketAddress, int32) {
	switch a := sa.(type) {
	case *unix.SockaddrInet4:
		return socketAddress{family: AFInet4, lo: uint64(binary.BigEndian.Uint32(a.Addr[:])), port: uint16(a.Port)}, ErrOK
	case *unix.SockaddrInet6:
		return socketAddress{
			family: AFInet6,
			hi:     binary.BigEndian.Uint64(a.Addr[:8]),
			lo:     binary.BigEndian.Uint64(a.Addr[8:]),
			port:   uint16(a.Port),
			scope:  a.ZoneId,
		}, ErrOK
	default:
		return socketAddress{}, ErrAddressInvalid
	}
}

func sameAddress(a, b socketAddress) bool {
	return a.family == b.family && a.hi == b.hi && a.lo == b.lo && a.port == b.port && a.scope == b.scope
}

func (p *Plugin) socketOpenHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 4 || len(results) < 2 {
		panic(wago.HostTrap{Err: errors.New("facet: socket_open host signature mismatch")})
	}
	family := int32(uint32(params[0]))
	stype := int32(uint32(params[1]))
	protocol := int32(uint32(params[2]))
	flags := uint32(params[3])
	if flags&^SockNonblock != 0 {
		results[1] = uint64(uint32(ErrInvalid))
		return
	}
	domain, ok := socketDomain(family)
	if !ok {
		results[1] = uint64(uint32(ErrAddressInvalid))
		return
	}
	osType, osProto, code := socketTypeAndProtocol(stype, protocol)
	if code != ErrOK {
		results[1] = uint64(uint32(code))
		return
	}
	fd, err := unix.Socket(domain, osType|unix.SOCK_CLOEXEC, osProto)
	if err != nil {
		results[1] = uint64(uint32(errorCode(err)))
		return
	}
	nonblock := flags&SockNonblock != 0
	if nonblock {
		if err := unix.SetNonblock(fd, true); err != nil {
			_ = unix.Close(fd)
			results[1] = uint64(uint32(errorCode(err)))
			return
		}
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	id, code := state.alloc(&handleEntry{
		kind:   handleSocket,
		rights: RightRead | RightWrite | RightStat,
		flags: func() uint32 {
			if nonblock {
				return FDNonblock
			}
			return 0
		}(),
		sock: &socketResource{fd: fd, family: family, stype: stype, protocol: protocol, state: socketOpen, nonblock: nonblock},
	})
	if code != ErrOK {
		_ = unix.Close(fd)
		results[1] = uint64(uint32(code))
		return
	}
	results[0] = uint64(id)
	results[1] = uint64(uint32(ErrOK))
}

func (p *Plugin) withSocket(m wago.HostModule, raw uint64, fn func(*instanceState, uint32, *handleEntry, *socketResource) int32) int32 {
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	id := uint32(raw)
	h, code := state.get(id)
	if code != ErrOK || h.sock == nil || h.kind != handleSocket {
		return ErrBadHandle
	}
	return fn(state, id, h, h.sock)
}

func (p *Plugin) socketBindHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 6 || len(results) < 1 {
		panic(wago.HostTrap{Err: errors.New("facet: socket_bind host signature mismatch")})
	}
	address, code := addressFromFields(int32(uint32(params[1])), params[2], params[3], uint32(params[4]), uint32(params[5]))
	if code != ErrOK {
		results[0] = uint64(uint32(code))
		return
	}
	code = p.withSocket(m, params[0], func(_ *instanceState, _ uint32, _ *handleEntry, sock *socketResource) int32 {
		if address.family != sock.family {
			return ErrAddressInvalid
		}
		if sock.state != socketOpen {
			return ErrInvalid
		}
		sa, err := address.sockaddr()
		if err != nil {
			return errorCode(err)
		}
		if err := unix.Bind(sock.fd, sa); err != nil {
			return errorCode(err)
		}
		sock.state = socketBound
		return ErrOK
	})
	results[0] = uint64(uint32(code))
}

func (p *Plugin) socketConnectHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 6 || len(results) < 1 {
		panic(wago.HostTrap{Err: errors.New("facet: socket_connect host signature mismatch")})
	}
	address, code := addressFromFields(int32(uint32(params[1])), params[2], params[3], uint32(params[4]), uint32(params[5]))
	if code != ErrOK {
		results[0] = uint64(uint32(code))
		return
	}
	code = p.withSocket(m, params[0], func(_ *instanceState, _ uint32, _ *handleEntry, sock *socketResource) int32 {
		if address.family != sock.family {
			return ErrAddressInvalid
		}
		switch sock.state {
		case socketConnected:
			if sock.hasPending && sameAddress(sock.pending, address) {
				return ErrOK
			}
			return ErrBusy
		case socketConnecting:
			if !sock.hasPending || !sameAddress(sock.pending, address) {
				return ErrBusy
			}
			soerr, err := unix.GetsockoptInt(sock.fd, unix.SOL_SOCKET, unix.SO_ERROR)
			if err != nil {
				return errorCode(err)
			}
			if soerr == 0 {
				sock.state = socketConnected
				return ErrOK
			}
			e := syscall.Errno(soerr)
			if errors.Is(e, syscall.EINPROGRESS) || errors.Is(e, syscall.EALREADY) || errors.Is(e, syscall.EAGAIN) {
				return ErrAgain
			}
			sock.state = socketFailed
			return errorCode(e)
		case socketFailed:
			return ErrNotConnected
		case socketListening:
			return ErrInvalid
		}
		sa, err := address.sockaddr()
		if err != nil {
			return errorCode(err)
		}
		err = unix.Connect(sock.fd, sa)
		if err == nil || errors.Is(err, syscall.EISCONN) {
			sock.state = socketConnected
			sock.pending = address
			sock.hasPending = true
			return ErrOK
		}
		if sock.nonblock && (errors.Is(err, syscall.EINPROGRESS) || errors.Is(err, syscall.EALREADY) || errors.Is(err, syscall.EAGAIN)) {
			sock.state = socketConnecting
			sock.pending = address
			sock.hasPending = true
			return ErrAgain
		}
		sock.state = socketFailed
		sock.pending = address
		sock.hasPending = true
		return errorCode(err)
	})
	results[0] = uint64(uint32(code))
}

func (p *Plugin) socketListenHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 2 || len(results) < 1 {
		panic(wago.HostTrap{Err: errors.New("facet: socket_listen host signature mismatch")})
	}
	backlog := uint32(params[1])
	if backlog > math.MaxInt32 {
		results[0] = uint64(uint32(ErrInvalid))
		return
	}
	code := p.withSocket(m, params[0], func(_ *instanceState, _ uint32, _ *handleEntry, sock *socketResource) int32 {
		if sock.stype != SockStream {
			return ErrProtocol
		}
		if sock.state != socketBound {
			return ErrInvalid
		}
		if err := unix.Listen(sock.fd, int(backlog)); err != nil {
			return errorCode(err)
		}
		sock.state = socketListening
		return ErrOK
	})
	results[0] = uint64(uint32(code))
}

func (p *Plugin) socketAcceptHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 2 || len(results) < 7 {
		panic(wago.HostTrap{Err: errors.New("facet: socket_accept host signature mismatch")})
	}
	flags := uint32(params[1])
	if flags&^SockNonblock != 0 {
		results[6] = uint64(uint32(ErrInvalid))
		return
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	h, code := state.get(uint32(params[0]))
	if code != ErrOK || h.sock == nil || h.kind != handleSocket {
		results[6] = uint64(uint32(ErrBadHandle))
		return
	}
	listener := h.sock
	if listener.stype != SockStream {
		results[6] = uint64(uint32(ErrProtocol))
		return
	}
	if listener.state != socketListening {
		results[6] = uint64(uint32(ErrInvalid))
		return
	}
	acceptFlags := unix.SOCK_CLOEXEC
	if flags&SockNonblock != 0 {
		acceptFlags |= unix.SOCK_NONBLOCK
	}
	fd, sa, err := unix.Accept4(listener.fd, acceptFlags)
	if err != nil {
		results[6] = uint64(uint32(errorCode(err)))
		return
	}
	address, code := addressFromSockaddr(sa)
	if code != ErrOK {
		_ = unix.Close(fd)
		results[6] = uint64(uint32(code))
		return
	}
	client := &socketResource{fd: fd, family: listener.family, stype: SockStream, protocol: listener.protocol, state: socketConnected, nonblock: flags&SockNonblock != 0}
	id, code := state.alloc(&handleEntry{kind: handleSocket, rights: RightRead | RightWrite | RightStat, flags: func() uint32 {
		if client.nonblock {
			return FDNonblock
		}
		return 0
	}(), sock: client})
	if code != ErrOK {
		_ = unix.Close(fd)
		results[6] = uint64(uint32(code))
		return
	}
	results[0] = uint64(id)
	results[1] = uint64(uint32(address.family))
	results[2] = address.hi
	results[3] = address.lo
	results[4] = uint64(address.port)
	results[5] = uint64(address.scope)
	results[6] = uint64(uint32(ErrOK))
}

func writeAddressResults(results []uint64, a socketAddress, code int32) {
	zeroResults(results)
	if len(results) < 6 {
		return
	}
	if code != ErrOK {
		results[5] = uint64(uint32(code))
		return
	}
	results[0] = uint64(uint32(a.family))
	results[1] = a.hi
	results[2] = a.lo
	results[3] = uint64(a.port)
	results[4] = uint64(a.scope)
	results[5] = uint64(uint32(ErrOK))
}

func (p *Plugin) socketLocalAddressHost(m wago.HostModule, params, results []uint64) {
	if len(params) != 1 || len(results) < 6 {
		panic(wago.HostTrap{Err: errors.New("facet: socket_local_address host signature mismatch")})
	}
	var address socketAddress
	code := p.withSocket(m, params[0], func(_ *instanceState, _ uint32, _ *handleEntry, sock *socketResource) int32 {
		if sock.state == socketOpen {
			return ErrInvalid
		}
		sa, err := unix.Getsockname(sock.fd)
		if err != nil {
			return errorCode(err)
		}
		var c int32
		address, c = addressFromSockaddr(sa)
		return c
	})
	writeAddressResults(results, address, code)
}

func (p *Plugin) socketPeerAddressHost(m wago.HostModule, params, results []uint64) {
	if len(params) != 1 || len(results) < 6 {
		panic(wago.HostTrap{Err: errors.New("facet: socket_peer_address host signature mismatch")})
	}
	var address socketAddress
	code := p.withSocket(m, params[0], func(_ *instanceState, _ uint32, _ *handleEntry, sock *socketResource) int32 {
		if sock.state != socketConnected {
			return ErrNotConnected
		}
		sa, err := unix.Getpeername(sock.fd)
		if err != nil {
			return errorCode(err)
		}
		var c int32
		address, c = addressFromSockaddr(sa)
		return c
	})
	writeAddressResults(results, address, code)
}

func (p *Plugin) socketShutdownHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 2 || len(results) < 1 {
		panic(wago.HostTrap{Err: errors.New("facet: socket_shutdown host signature mismatch")})
	}
	how := int32(uint32(params[1]))
	osHow := -1
	switch how {
	case ShutRD:
		osHow = unix.SHUT_RD
	case ShutWR:
		osHow = unix.SHUT_WR
	case ShutRDWR:
		osHow = unix.SHUT_RDWR
	default:
		results[0] = uint64(uint32(ErrInvalid))
		return
	}
	code := p.withSocket(m, params[0], func(_ *instanceState, _ uint32, _ *handleEntry, sock *socketResource) int32 {
		if sock.stype != SockStream {
			return ErrProtocol
		}
		if sock.state != socketConnected {
			return ErrNotConnected
		}
		if err := unix.Shutdown(sock.fd, osHow); err != nil {
			return errorCode(err)
		}
		return ErrOK
	})
	results[0] = uint64(uint32(code))
}
