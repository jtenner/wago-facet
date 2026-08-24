package facet

import (
	"testing"

	wago "github.com/wago-org/wago"
)

func openDatagram(t *testing.T, p *Plugin, m wago.HostModule) uint64 {
	t.Helper()
	results := make([]uint64, 2)
	p.socketOpenHost(m, []uint64{uint64(AFInet4), uint64(SockDgram), uint64(ProtoUDP), 0}, results)
	if results[0] == 0 || int32(results[1]) != ErrOK {
		t.Fatalf("socket_open = %v", results)
	}
	return results[0]
}

func TestDatagramMemoryValidationPrecedesReceiveAndReportsTruncation(t *testing.T) {
	storage := &fakeGuestStorage{
		memoryInfo: wago.GuestMemoryInfo{AddressType: wago.GuestMemory32},
		memory:     make([]byte, 64),
	}
	copy(storage.memory, []byte("ping"))
	p, m := rawPluginWithStorage(Config{}, storage)
	defer p.raw.closeAll()

	receiver := openDatagram(t, p, m)
	bindResult := make([]uint64, 1)
	p.socketBindHost(m, []uint64{receiver, uint64(AFInet4), 0, 0x7f000001, 0, 0}, bindResult)
	if int32(bindResult[0]) != ErrOK {
		t.Fatalf("socket_bind = %v", bindResult)
	}
	local := make([]uint64, 6)
	p.socketLocalAddressHost(m, []uint64{receiver}, local)
	if int32(local[5]) != ErrOK || local[3] == 0 {
		t.Fatalf("socket_local_address = %v", local)
	}

	sender := openDatagram(t, p, m)
	send := make([]uint64, 2)
	p.socketSendMemoryHost(wago.GuestMemory32)(m, []uint64{
		sender, 0, 0, 4,
		local[0], local[1], local[2], local[3], local[4], 0,
	}, send)
	if send[0] != 4 || int32(send[1]) != ErrOK {
		t.Fatalf("socket_sendto = %v", send)
	}

	invalid := make([]uint64, 8)
	p.socketRecvMemoryHost(wago.GuestMemory32)(m, []uint64{receiver, 0, 63, 4, 0}, invalid)
	if invalid[0] != 0 || int32(invalid[7]) != ErrFault {
		t.Fatalf("out-of-bounds socket_recvfrom = %v", invalid)
	}

	recv := make([]uint64, 8)
	p.socketRecvMemoryHost(wago.GuestMemory32)(m, []uint64{receiver, 0, 16, 2, 0}, recv)
	if recv[0] != 2 || int32(recv[7]) != ErrOK {
		t.Fatalf("socket_recvfrom = %v", recv)
	}
	if got := string(storage.memory[16:18]); got != "pi" {
		t.Fatalf("received prefix = %q, want %q", got, "pi")
	}
	if uint32(recv[6])&msgTruncated == 0 {
		t.Fatalf("message flags = %#x, want MSG_TRUNCATED", recv[6])
	}
	if int32(uint32(recv[1])) != AFInet4 {
		t.Fatalf("source family = %d, want AF_INET4", recv[1])
	}
}

func TestDatagramRejectsUnknownFlagsBeforeHandleLookup(t *testing.T) {
	storage := &fakeGuestStorage{
		memoryInfo: wago.GuestMemoryInfo{AddressType: wago.GuestMemory32},
		memory:     make([]byte, 1),
	}
	p, m := rawPluginWithStorage(Config{}, storage)

	recv := make([]uint64, 8)
	p.socketRecvMemoryHost(wago.GuestMemory32)(m, []uint64{0, 0, 0, 1, 1}, recv)
	if int32(recv[7]) != ErrInvalid {
		t.Fatalf("recv flags precedence = %v", recv)
	}

	send := make([]uint64, 2)
	p.socketSendMemoryHost(wago.GuestMemory32)(m, []uint64{0, 0, 0, 1, uint64(AFInet4), 0, 0, 1, 0, 1}, send)
	if int32(send[1]) != ErrInvalid {
		t.Fatalf("send flags precedence = %v", send)
	}
}

func TestDatagramBindings(t *testing.T) {
	bindings := (&Plugin{}).datagramBindings()
	if len(bindings) != 14 {
		t.Fatalf("datagram binding count = %d, want 14", len(bindings))
	}
	seen := make(map[string]struct{}, len(bindings))
	for _, b := range bindings {
		if _, duplicate := seen[b.name]; duplicate {
			t.Fatalf("duplicate datagram binding %q", b.name)
		}
		seen[b.name] = struct{}{}
	}
	for _, name := range []string{
		"socket_recvfrom_mem32",
		"socket_recvfrom_mem64",
		"socket_recvfrom_array_v128",
		"socket_sendto_mem32",
		"socket_sendto_mem64",
		"socket_sendto_array_i8",
	} {
		if _, ok := seen[name]; !ok {
			t.Fatalf("missing datagram binding %q", name)
		}
		if _, ok := Imports(Config{})[Module+"."+name]; ok {
			t.Fatalf("raw Imports unexpectedly advertises plugin-only binding %q", name)
		}
	}
}
