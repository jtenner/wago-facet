package facet

import (
	"context"
	"encoding/binary"
	"errors"
	"net"

	wago "github.com/wago-org/wago"
)

type dnsResolver struct {
	addresses []socketAddress
	index     int
}

func dnsFamilyValid(family int32) bool {
	return family == AFUnspec || family == AFInet4 || family == AFInet6
}

func ipAddrToSocketAddress(ip net.IPAddr) (socketAddress, bool) {
	if v4 := ip.IP.To4(); v4 != nil {
		return socketAddress{family: AFInet4, lo: uint64(binary.BigEndian.Uint32(v4))}, true
	}
	v6 := ip.IP.To16()
	if v6 == nil {
		return socketAddress{}, false
	}
	var scope uint32
	if ip.Zone != "" {
		if iface, err := net.InterfaceByName(ip.Zone); err == nil && iface.Index > 0 {
			scope = uint32(iface.Index)
		}
	}
	return socketAddress{
		family: AFInet6,
		hi:     binary.BigEndian.Uint64(v6[:8]),
		lo:     binary.BigEndian.Uint64(v6[8:]),
		scope:  scope,
	}, true
}

func (p *Plugin) dnsResolveDecoded(m wago.HostModule, hostname string, family int32, flags uint32, results []uint64) {
	zeroResults(results)
	if !dnsFamilyValid(family) || flags != 0 {
		results[1] = uint64(uint32(ErrInvalid))
		return
	}
	if code := validateDNSName(hostname); code != ErrOK {
		results[1] = uint64(uint32(code))
		return
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(context.Background(), hostname)
	if err != nil {
		results[1] = uint64(uint32(errorCode(err)))
		return
	}
	resolved := make([]socketAddress, 0, len(addresses))
	for _, ip := range addresses {
		a, ok := ipAddrToSocketAddress(ip)
		if !ok || (family != AFUnspec && a.family != family) {
			continue
		}
		resolved = append(resolved, a)
	}
	if len(resolved) == 0 {
		results[1] = uint64(uint32(ErrNoEntry))
		return
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	id, code := state.alloc(&handleEntry{kind: handleResolver, dns: &dnsResolver{addresses: resolved}})
	results[0] = uint64(id)
	results[1] = uint64(uint32(code))
}

func (p *Plugin) dnsResolveMemoryHost(width textWidth, addressType wago.GuestMemoryAddressType) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 6 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: dns_resolve memory signature mismatch")})
		}
		wtf := int32(uint32(params[3]))
		family := int32(uint32(params[4]))
		flags := uint32(params[5])
		if !validWTF(wtf) || !dnsFamilyValid(family) || flags != 0 {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		pointer, units := params[1], params[2]
		if addressType == wago.GuestMemory32 {
			pointer, units = uint64(uint32(pointer)), uint64(uint32(units))
		}
		hostname, code := readGuestTextMemory(m, width, addressType, uint32(params[0]), pointer, units, wtf)
		if code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		p.dnsResolveDecoded(m, hostname, family, flags, results)
	}
}

func (p *Plugin) dnsResolveArrayHost(width textWidth) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 6 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: dns_resolve array signature mismatch")})
		}
		wtf := int32(uint32(params[3]))
		family := int32(uint32(params[4]))
		flags := uint32(params[5])
		if !validWTF(wtf) || !dnsFamilyValid(family) || flags != 0 {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		hostname, code := readGuestTextArray(m, width, params[0], uint32(params[1]), uint32(params[2]), wtf)
		if code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		p.dnsResolveDecoded(m, hostname, family, flags, results)
	}
}

func (p *Plugin) dnsNextHost(m wago.HostModule, params, results []uint64) {
	zeroResults(results)
	if len(params) != 1 || len(results) < 6 {
		panic(wago.HostTrap{Err: errors.New("facet: dns_next signature mismatch")})
	}
	state := p.stateFor(m)
	state.mu.Lock()
	defer state.mu.Unlock()
	h, code := state.get(uint32(params[0]))
	if code != ErrOK || h.kind != handleResolver || h.dns == nil {
		results[5] = uint64(uint32(ErrBadHandle))
		return
	}
	resolver := h.dns
	if resolver.index >= len(resolver.addresses) {
		results[4] = 1
		results[5] = uint64(uint32(ErrOK))
		return
	}
	a := resolver.addresses[resolver.index]
	resolver.index++
	results[0] = uint64(uint32(a.family))
	results[1] = a.hi
	results[2] = a.lo
	results[3] = uint64(a.scope)
	results[5] = uint64(uint32(ErrOK))
}

func (p *Plugin) dnsBindings() []binding {
	i32, i64, anyref := wago.ValI32, wago.ValI64, wago.ValAnyRef
	out := make([]binding, 0, 10)
	for _, spec := range []struct {
		suffix string
		width  textWidth
	}{{"i8", textI8}, {"i16", textI16}, {"i32", textI32}} {
		out = append(out,
			binding{"dns_resolve_mem32_" + spec.suffix, p.dnsResolveMemoryHost(spec.width, wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32, i32, i32}, []wago.ValType{i32, i32}, CapNetwork, "resolve an ASCII Memory32 DNS name"},
			binding{"dns_resolve_mem64_" + spec.suffix, p.dnsResolveMemoryHost(spec.width, wago.GuestMemory64), []wago.ValType{i32, i64, i64, i32, i32, i32}, []wago.ValType{i32, i32}, CapNetwork, "resolve an ASCII Memory64 DNS name"},
			binding{"dns_resolve_array_" + spec.suffix, p.dnsResolveArrayHost(spec.width), []wago.ValType{anyref, i32, i32, i32, i32, i32}, []wago.ValType{i32, i32}, CapNetwork, "resolve an ASCII GC-array DNS name"},
		)
	}
	out = append(out, binding{"dns_next", p.dnsNextHost, []wago.ValType{i32}, []wago.ValType{i32, i64, i64, i32, i32, i32}, CapNetwork, "return the next resolved address"})
	return out
}
