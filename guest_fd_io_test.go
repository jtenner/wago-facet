package facet

import (
	"bytes"
	"testing"

	wago "github.com/wago-org/wago"
)

func rawPluginWithStorage(cfg Config, storage *fakeGuestStorage) (*Plugin, *fakeGuestStorageModule) {
	cfg = normalizeConfig(cfg)
	return &Plugin{cfg: cfg, raw: newInstanceState(cfg)}, &fakeGuestStorageModule{storage: storage}
}

func stdioHandle(t *testing.T, p *Plugin, m wago.HostModule, which int) uint64 {
	t.Helper()
	results := make([]uint64, 2)
	p.stdioHost(m, which, nil, results)
	if results[0] == 0 || int32(results[1]) != ErrOK {
		t.Fatalf("stdio(%d) = %v", which, results)
	}
	return results[0]
}

func TestFDReadMemoryValidatesRangeBeforeConsumingStream(t *testing.T) {
	stdin := bytes.NewBufferString("abc")
	storage := &fakeGuestStorage{
		memoryInfo: wago.GuestMemoryInfo{AddressType: wago.GuestMemory32},
		memory:     make([]byte, 2),
	}
	p, m := rawPluginWithStorage(Config{Stdin: stdin}, storage)
	fd := stdioHandle(t, p, m, 0)
	read := p.fdMemoryIOHost(fdIORead, wago.GuestMemory32)

	invalid := make([]uint64, 2)
	read(m, []uint64{fd, 0, 0, 3}, invalid)
	if invalid[0] != 0 || int32(invalid[1]) != ErrFault {
		t.Fatalf("out-of-bounds read = %v", invalid)
	}
	if got := stdin.String(); got != "abc" {
		t.Fatalf("invalid guest range consumed stdin: %q", got)
	}

	valid := make([]uint64, 2)
	read(m, []uint64{fd, 0, 0, 2}, valid)
	if valid[0] != 2 || int32(valid[1]) != ErrOK {
		t.Fatalf("valid read = %v", valid)
	}
	if got := string(storage.memory); got != "ab" {
		t.Fatalf("guest memory = %q, want %q", got, "ab")
	}
	if got := stdin.String(); got != "c" {
		t.Fatalf("stdin remainder = %q, want %q", got, "c")
	}
}

func TestFDReadEOFIsSuccess(t *testing.T) {
	storage := &fakeGuestStorage{
		memoryInfo: wago.GuestMemoryInfo{AddressType: wago.GuestMemory32},
		memory:     make([]byte, 4),
	}
	p, m := rawPluginWithStorage(Config{Stdin: bytes.NewReader(nil)}, storage)
	fd := stdioHandle(t, p, m, 0)
	results := make([]uint64, 2)
	p.fdMemoryIOHost(fdIORead, wago.GuestMemory32)(m, []uint64{fd, 0, 0, 4}, results)
	if results[0] != 0 || int32(results[1]) != ErrOK {
		t.Fatalf("EOF read = %v", results)
	}
}

func TestFDWriteFromImmutableArraySource(t *testing.T) {
	var stdout bytes.Buffer
	storage := &fakeGuestStorage{
		arrayInfo: wago.GuestGCArrayInfo{Storage: wago.GuestGCArrayI16, Length: 3, Mutable: false},
		array:     []byte{'A', 0, 'B', 0, 'C', 0},
	}
	p, m := rawPluginWithStorage(Config{Stdout: &stdout}, storage)
	fd := stdioHandle(t, p, m, 1)
	results := make([]uint64, 2)
	p.fdArrayIOHost(fdIOWrite, wago.GuestGCArrayI16)(m, []uint64{fd, 1, 1, 4}, results)
	if results[0] != 4 || int32(results[1]) != ErrOK {
		t.Fatalf("array write = %v", results)
	}
	if got := stdout.Bytes(); !bytes.Equal(got, []byte{0, 'B', 0, 'C'}) {
		t.Fatalf("stdout = %v", got)
	}
}

func TestFDReadChecksRightsBeforeGuestRepresentation(t *testing.T) {
	var stdout bytes.Buffer
	storage := &fakeGuestStorage{
		memoryInfo: wago.GuestMemoryInfo{AddressType: wago.GuestMemory64},
		memory:     make([]byte, 1),
	}
	p, m := rawPluginWithStorage(Config{Stdout: &stdout}, storage)
	fd := stdioHandle(t, p, m, 1)
	results := make([]uint64, 2)
	p.fdMemoryIOHost(fdIORead, wago.GuestMemory32)(m, []uint64{fd, 0, 0, 1}, results)
	if results[0] != 0 || int32(results[1]) != ErrCapability {
		t.Fatalf("read through stdout = %v, want ERR_CAPABILITY before representation validation", results)
	}
}

func TestFDIOBindings(t *testing.T) {
	bindings := (&Plugin{}).fdIOBindings()
	if len(bindings) != 14 {
		t.Fatalf("descriptor I/O binding count = %d, want 14", len(bindings))
	}
	seen := make(map[string]struct{}, len(bindings))
	for _, b := range bindings {
		if _, duplicate := seen[b.name]; duplicate {
			t.Fatalf("duplicate descriptor I/O binding %q", b.name)
		}
		seen[b.name] = struct{}{}
	}
	for _, name := range []string{
		"fd_read_mem32",
		"fd_read_mem64",
		"fd_read_array_v128",
		"fd_write_mem32",
		"fd_write_mem64",
		"fd_write_array_i8",
	} {
		if _, ok := seen[name]; !ok {
			t.Fatalf("missing descriptor I/O binding %q", name)
		}
		if _, ok := Imports(Config{})[Module+"."+name]; ok {
			t.Fatalf("raw Imports unexpectedly advertises plugin-only binding %q", name)
		}
	}
}
