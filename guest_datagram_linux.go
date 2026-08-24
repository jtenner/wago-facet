package facet

import (
	"errors"

	wago "github.com/wago-org/wago"
	"golang.org/x/sys/unix"
)

const msgTruncated uint32 = 1 << 0

func validateDatagramSocket(h *handleEntry, write bool) (*socketResource, int32) {
	if h == nil || h.sock == nil {
		return nil, ErrBadHandle
	}
	right := uint64(RightRead)
	if write {
		right = RightWrite
	}
	if h.rights&right == 0 {
		return nil, ErrCapability
	}
	if h.sock.stype != SockDgram {
		return nil, ErrProtocol
	}
	return h.sock, ErrOK
}

func (p *Plugin) withDatagramSocket(m wago.HostModule, raw uint64, write bool, fn func(*socketResource) int32) int32 {
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	h, code := getFD(state, uint32(raw))
	if code != ErrOK {
		return code
	}
	sock, code := validateDatagramSocket(h, write)
	if code != ErrOK {
		return code
	}
	return fn(sock)
}

func recvDatagram(sock *socketResource, buf []byte) (uint64, socketAddress, uint32, int32) {
	n, _, recvFlags, from, err := unix.Recvmsg(sock.fd, buf, nil, 0)
	if err != nil {
		return 0, socketAddress{}, 0, errorCode(err)
	}
	address, code := addressFromSockaddr(from)
	if code != ErrOK {
		return 0, socketAddress{}, 0, code
	}
	flags := uint32(0)
	if recvFlags&unix.MSG_TRUNC != 0 {
		flags |= msgTruncated
	}
	return uint64(n), address, flags, ErrOK
}

func sendDatagram(sock *socketResource, buf []byte, address socketAddress) (uint64, int32) {
	sa, err := address.sockaddr()
	if err != nil {
		return 0, errorCode(err)
	}
	if err := unix.Sendto(sock.fd, buf, 0, sa); err != nil {
		return 0, errorCode(err)
	}
	return uint64(len(buf)), ErrOK
}

func writeRecvResults(results []uint64, n uint64, address socketAddress, flags uint32, code int32) {
	if code != ErrOK {
		results[7] = uint64(uint32(code))
		return
	}
	results[0] = n
	results[1] = uint64(uint32(address.family))
	results[2] = address.hi
	results[3] = address.lo
	results[4] = uint64(uint32(address.port))
	results[5] = uint64(address.scope)
	results[6] = uint64(flags)
	results[7] = uint64(uint32(ErrOK))
}

func (p *Plugin) socketRecvMemoryHost(addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 5 || len(results) < 8 {
			panic(wago.HostTrap{Err: errors.New("facet: socket_recvfrom memory host signature mismatch")})
		}
		if uint32(params[4]) != 0 {
			results[7] = uint64(uint32(ErrInvalid))
			return
		}

		memoryIndex := uint32(params[1])
		pointer := params[2]
		length := params[3]
		if addressType == wago.GuestMemory32 {
			pointer = uint64(uint32(pointer))
			length = uint64(uint32(length))
		}

		var n uint64
		var address socketAddress
		var messageFlags uint32
		code := p.withDatagramSocket(m, params[0], false, func(sock *socketResource) int32 {
			return memoryRange(m, addressType, memoryIndex, pointer, length, wago.GuestStorageWrite, func(buf []byte) int32 {
				var ioCode int32
				n, address, messageFlags, ioCode = recvDatagram(sock, buf)
				return ioCode
			})
		})
		writeRecvResults(results, n, address, messageFlags, code)
	}
}

func (p *Plugin) socketRecvArrayHost(storageClass wago.GuestGCArrayStorage) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 5 || len(results) < 8 {
			panic(wago.HostTrap{Err: errors.New("facet: socket_recvfrom array host signature mismatch")})
		}
		if uint32(params[4]) != 0 {
			results[7] = uint64(uint32(ErrInvalid))
			return
		}

		var n uint64
		var address socketAddress
		var messageFlags uint32
		code := p.withDatagramSocket(m, params[0], false, func(sock *socketResource) int32 {
			return arrayRange(m, params[1], storageClass, params[2], params[3], wago.GuestStorageWrite, func(buf []byte) int32 {
				var ioCode int32
				n, address, messageFlags, ioCode = recvDatagram(sock, buf)
				return ioCode
			})
		})
		writeRecvResults(results, n, address, messageFlags, code)
	}
}

func sendAddress(params []uint64, familyPos, hiPos, loPos, portPos, scopePos int) (socketAddress, int32) {
	return addressFromFields(
		int32(uint32(params[familyPos])),
		params[hiPos],
		params[loPos],
		uint32(params[portPos]),
		uint32(params[scopePos]),
	)
}

func (p *Plugin) socketSendMemoryHost(addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 10 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: socket_sendto memory host signature mismatch")})
		}
		if uint32(params[9]) != 0 {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		address, code := sendAddress(params, 4, 5, 6, 7, 8)
		if code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}

		memoryIndex := uint32(params[1])
		pointer := params[2]
		length := params[3]
		if addressType == wago.GuestMemory32 {
			pointer = uint64(uint32(pointer))
			length = uint64(uint32(length))
		}

		var n uint64
		code = p.withDatagramSocket(m, params[0], true, func(sock *socketResource) int32 {
			if address.family != sock.family {
				return ErrAddressInvalid
			}
			return memoryRange(m, addressType, memoryIndex, pointer, length, wago.GuestStorageRead, func(buf []byte) int32 {
				var ioCode int32
				n, ioCode = sendDatagram(sock, buf, address)
				return ioCode
			})
		})
		if code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		results[0] = n
		results[1] = uint64(uint32(ErrOK))
	}
}

func (p *Plugin) socketSendArrayHost(storageClass wago.GuestGCArrayStorage) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 10 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: socket_sendto array host signature mismatch")})
		}
		if uint32(params[9]) != 0 {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		address, code := sendAddress(params, 4, 5, 6, 7, 8)
		if code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}

		var n uint64
		code = p.withDatagramSocket(m, params[0], true, func(sock *socketResource) int32 {
			if address.family != sock.family {
				return ErrAddressInvalid
			}
			return arrayRange(m, params[1], storageClass, params[2], params[3], wago.GuestStorageRead, func(buf []byte) int32 {
				var ioCode int32
				n, ioCode = sendDatagram(sock, buf, address)
				return ioCode
			})
		})
		if code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		results[0] = n
		results[1] = uint64(uint32(ErrOK))
	}
}

func (p *Plugin) datagramBindings() []binding {
	i32 := wago.ValI32
	i64 := wago.ValI64
	anyref := wago.ValAnyRef
	recvResults := []wago.ValType{i64, i32, i64, i64, i32, i32, i32, i32}
	out := []binding{
		{"socket_recvfrom_mem32", p.socketRecvMemoryHost(wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32, i32}, recvResults, CapNetwork, "receive one datagram into indexed Memory32"},
		{"socket_recvfrom_mem64", p.socketRecvMemoryHost(wago.GuestMemory64), []wago.ValType{i32, i32, i64, i64, i32}, recvResults, CapNetwork, "receive one datagram into indexed Memory64"},
		{"socket_sendto_mem32", p.socketSendMemoryHost(wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32, i32, i64, i64, i32, i32, i32}, []wago.ValType{i64, i32}, CapNetwork, "send one datagram from indexed Memory32"},
		{"socket_sendto_mem64", p.socketSendMemoryHost(wago.GuestMemory64), []wago.ValType{i32, i32, i64, i64, i32, i64, i64, i32, i32, i32}, []wago.ValType{i64, i32}, CapNetwork, "send one datagram from indexed Memory64"},
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
			binding{"socket_recvfrom_array_" + spec.suffix, p.socketRecvArrayHost(spec.storage), []wago.ValType{i32, anyref, i64, i64, i32}, recvResults, CapNetwork, "receive one datagram into a mutable GC array"},
			binding{"socket_sendto_array_" + spec.suffix, p.socketSendArrayHost(spec.storage), []wago.ValType{i32, anyref, i64, i64, i32, i64, i64, i32, i32, i32}, []wago.ValType{i64, i32}, CapNetwork, "send one datagram from a GC array"},
		)
	}
	return out
}
