package facet

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	wago "github.com/wago-org/wago"
)

type fakeGuestStorage struct {
	memoryInfo wago.GuestMemoryInfo
	memory     []byte
	arrayInfo  wago.GuestGCArrayInfo
	array      []byte
	refErr     error
}

func (s *fakeGuestStorage) MemoryInfo(index uint32) (wago.GuestMemoryInfo, error) {
	if index != 0 {
		return wago.GuestMemoryInfo{}, errors.New("bad memory")
	}
	info := s.memoryInfo
	info.ByteLength = uint64(len(s.memory))
	return info, nil
}

func (s *fakeGuestStorage) MemoryRange(index uint32, offset, length uint64, _ wago.GuestStorageAccess) ([]byte, error) {
	if index != 0 || offset > uint64(len(s.memory)) || length > uint64(len(s.memory))-offset {
		return nil, errors.New("bad range")
	}
	return s.memory[offset : offset+length], nil
}

func (s *fakeGuestStorage) GCRef(uint64) (wago.GuestGCRef, error) {
	return wago.GuestGCRef{}, s.refErr
}

func (s *fakeGuestStorage) GCArrayInfo(wago.GuestGCRef) (wago.GuestGCArrayInfo, error) {
	return s.arrayInfo, nil
}

func (s *fakeGuestStorage) GCArrayBytes(_ wago.GuestGCRef, access wago.GuestStorageAccess) ([]byte, wago.GuestGCArrayInfo, error) {
	if access == wago.GuestStorageWrite && !s.arrayInfo.Mutable {
		return nil, wago.GuestGCArrayInfo{}, errors.New("immutable")
	}
	return s.array, s.arrayInfo, nil
}

func (*fakeGuestStorage) GCArrayRef(wago.GuestGCRef, uint32) (wago.GuestGCRef, error) {
	return wago.GuestGCRef{}, errors.New("not implemented")
}

func (*fakeGuestStorage) ImportParamType(int) (wago.ValueTypeDescriptor, bool) {
	return wago.ValueTypeDescriptor{}, false
}

func (*fakeGuestStorage) ImportResultType(int) (wago.ValueTypeDescriptor, bool) {
	return wago.ValueTypeDescriptor{}, false
}

func (*fakeGuestStorage) DefinedType(uint32) (wago.DefinedTypeDescriptor, bool) {
	return wago.DefinedTypeDescriptor{}, false
}

type fakeGuestStorageModule struct{ storage *fakeGuestStorage }

func (*fakeGuestStorageModule) Memory() []byte { return nil }

func (m *fakeGuestStorageModule) WithGuestStorage(fn func(wago.GuestStorage) error) error {
	return fn(m.storage)
}

func TestCopyTextToIndexedMemory(t *testing.T) {
	storage := &fakeGuestStorage{
		memoryInfo: wago.GuestMemoryInfo{AddressType: wago.GuestMemory32},
		memory:     make([]byte, 16),
	}
	m := &fakeGuestStorageModule{storage: storage}
	units, code := copyTextToMemory(m, "caf\u00e9", textI16, 0, textMemoryDestination{
		addressType: wago.GuestMemory32,
		memoryIndex: 0,
		pointer:     2,
		capacity:    5,
	})
	if units != 4 || code != ErrOK {
		t.Fatalf("copy = units %d code %d", units, code)
	}
	want := []byte{'c', 0, 'a', 0, 'f', 0, 0xe9, 0}
	for i, b := range want {
		if got := storage.memory[2+i]; got != b {
			t.Fatalf("memory[%d] = %#x, want %#x", 2+i, got, b)
		}
	}
	if storage.memory[10] != 0 {
		t.Fatalf("storage after represented text was modified: %#x", storage.memory[10])
	}
}

func TestTextMemoryValidationPrecedesMutation(t *testing.T) {
	storage := &fakeGuestStorage{
		memoryInfo: wago.GuestMemoryInfo{AddressType: wago.GuestMemory32},
		memory:     []byte{0xaa, 0xbb, 0xcc, 0xdd},
	}
	m := &fakeGuestStorageModule{storage: storage}
	before := append([]byte(nil), storage.memory...)

	if units, code := copyTextToMemory(m, "ab", textI8, 0, textMemoryDestination{
		addressType: wago.GuestMemory32,
		memoryIndex: 0,
		pointer:     0,
		capacity:    1,
	}); units != 0 || code != ErrRange {
		t.Fatalf("short capacity = units %d code %d", units, code)
	}
	for i := range before {
		if storage.memory[i] != before[i] {
			t.Fatalf("short-capacity failure modified memory: %x", storage.memory)
		}
	}

	if units, code := copyTextToMemory(m, "x", textI8, 0, textMemoryDestination{
		addressType: wago.GuestMemory64,
		memoryIndex: 0,
		pointer:     0,
		capacity:    1,
	}); units != 0 || code != ErrType {
		t.Fatalf("address mismatch = units %d code %d", units, code)
	}
}

func TestCopyTextToGCArray(t *testing.T) {
	storage := &fakeGuestStorage{
		arrayInfo: wago.GuestGCArrayInfo{Storage: wago.GuestGCArrayI16, Length: 4, Mutable: true},
		array:     make([]byte, 8),
	}
	m := &fakeGuestStorageModule{storage: storage}
	units, code := copyTextToArray(m, "AB", textI16, 0, 1, 1, 2)
	if units != 2 || code != ErrOK {
		t.Fatalf("copy = units %d code %d", units, code)
	}
	want := []byte{0, 0, 'A', 0, 'B', 0, 0, 0}
	for i, b := range want {
		if storage.array[i] != b {
			t.Fatalf("array[%d] = %#x, want %#x", i, storage.array[i], b)
		}
	}

	storage.arrayInfo.Mutable = false
	if units, code = copyTextToArray(m, "A", textI16, 0, 1, 0, 1); units != 0 || code != ErrType {
		t.Fatalf("immutable destination = units %d code %d", units, code)
	}
}

func TestArrayRangeSupportsPartialElements(t *testing.T) {
	storage := &fakeGuestStorage{
		arrayInfo: wago.GuestGCArrayInfo{Storage: wago.GuestGCArrayI32, Length: 2, Mutable: true},
		array:     []byte{0, 1, 2, 3, 4, 5, 6, 7},
	}
	m := &fakeGuestStorageModule{storage: storage}
	code := arrayRange(m, 1, wago.GuestGCArrayI32, 3, 3, wago.GuestStorageWrite, func(buf []byte) int32 {
		copy(buf, []byte{9, 10, 11})
		return ErrOK
	})
	if code != ErrOK {
		t.Fatalf("arrayRange code = %d", code)
	}
	want := []byte{0, 1, 2, 9, 10, 11, 6, 7}
	for i, b := range want {
		if storage.array[i] != b {
			t.Fatalf("array[%d] = %d, want %d", i, storage.array[i], b)
		}
	}
}

func TestGuestStorageBindingsArePluginOnly(t *testing.T) {
	p := &Plugin{}
	bindings := p.guestStorageBindings()
	if len(bindings) != 43 {
		t.Fatalf("guest-storage binding count = %d, want 43", len(bindings))
	}
	seen := make(map[string]struct{}, len(bindings))
	for _, b := range bindings {
		if _, duplicate := seen[b.name]; duplicate {
			t.Fatalf("duplicate guest-storage binding %q", b.name)
		}
		seen[b.name] = struct{}{}
	}
	for _, name := range []string{
		"args_read_mem32_i8",
		"args_read_mem64_i32",
		"env_read_into_array_i16",
		"fs_preopen_name_read_mem32_i8",
		"dir_iter_next_into_array_i32",
		"random_fill_array_v128",
	} {
		if _, ok := seen[name]; !ok {
			t.Fatalf("missing guest-storage binding %q", name)
		}
		if _, ok := Imports(Config{})[Module+"."+name]; ok {
			t.Fatalf("raw Imports unexpectedly advertises plugin-only binding %q", name)
		}
	}
}

func TestDirectoryReadConsumesOnlyAfterSuccessfulCopy(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := normalizeConfig(Config{Preopens: []Preopen{{Guest: "~", Host: dir, Rights: RightStat | RightDirIterate}}})
	p := &Plugin{cfg: cfg, raw: newInstanceState(cfg)}
	storage := &fakeGuestStorage{
		memoryInfo: wago.GuestMemoryInfo{AddressType: wago.GuestMemory32},
		memory:     make([]byte, 32),
	}
	m := &fakeGuestStorageModule{storage: storage}

	pre := make([]uint64, 2)
	p.preopenGetHost(m, []uint64{0}, pre)
	iterResult := make([]uint64, 2)
	p.dirIterOpenHost(m, []uint64{pre[0]}, iterResult)
	if iterResult[0] == 0 || int32(iterResult[1]) != ErrOK {
		t.Fatalf("dir_iter_open = %v", iterResult)
	}

	read := p.dirIterNextReadMemoryHost(textI8, wago.GuestMemory32)
	short := make([]uint64, 5)
	read(m, []uint64{iterResult[0], 0, 0, 0, 1}, short)
	if int32(short[4]) != ErrRange {
		t.Fatalf("short read = %v", short)
	}

	ok := make([]uint64, 5)
	read(m, []uint64{iterResult[0], 0, 0, 0, 32}, ok)
	if ok[0] != uint64(len("hello.txt")) || int32(ok[4]) != ErrOK {
		t.Fatalf("successful read = %v", ok)
	}
	if got := string(storage.memory[:len("hello.txt")]); got != "hello.txt" {
		t.Fatalf("copied name = %q", got)
	}

	done := make([]uint64, 5)
	read(m, []uint64{iterResult[0], 0, 0, 0, 32}, done)
	if done[3] != 1 || int32(done[4]) != ErrOK {
		t.Fatalf("done read = %v", done)
	}
}
