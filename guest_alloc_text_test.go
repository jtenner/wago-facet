package facet

import (
	"errors"
	"testing"

	wago "github.com/wago-org/wago"
)

type fakeGCArrayAllocatorModule struct {
	storage  wago.GuestGCArrayStorage
	payload  []byte
	length   uint32
	token    uint64
	allocErr error
	calls    int
}

func (*fakeGCArrayAllocatorModule) Memory() []byte { return nil }

func (m *fakeGCArrayAllocatorModule) NewGCArrayResult(_ int, length uint32, initialize func([]byte, wago.GuestGCArrayInfo) error) (uint64, error) {
	m.calls++
	if m.allocErr != nil {
		return 0, m.allocErr
	}
	width := 1
	switch m.storage {
	case wago.GuestGCArrayI16:
		width = 2
	case wago.GuestGCArrayI32:
		width = 4
	case wago.GuestGCArrayI64:
		width = 8
	case wago.GuestGCArrayV128:
		width = 16
	}
	m.length = length
	m.payload = make([]byte, int(length)*width)
	if initialize != nil {
		if err := initialize(m.payload, wago.GuestGCArrayInfo{Storage: m.storage, Length: length}); err != nil {
			return 0, err
		}
	}
	if m.token == 0 {
		m.token = 0x1234
	}
	return m.token, nil
}

func TestDefinedTextArrayMatchesStorageClass(t *testing.T) {
	if !definedTextArrayMatches(wago.DefinedTypeDescriptor{
		Kind:  wago.CompositeTypeArray,
		Array: wago.FieldTypeDescriptor{Storage: wago.StorageTypeDescriptor{Packed: true, PackedType: wago.PackedTypeI8}},
	}, textI8) {
		t.Fatal("packed i8 array did not match i8 text")
	}
	if !definedTextArrayMatches(wago.DefinedTypeDescriptor{
		Kind:  wago.CompositeTypeArray,
		Array: wago.FieldTypeDescriptor{Storage: wago.StorageTypeDescriptor{Packed: true, PackedType: wago.PackedTypeI16}},
	}, textI16) {
		t.Fatal("packed i16 array did not match i16 text")
	}
	if !definedTextArrayMatches(wago.DefinedTypeDescriptor{
		Kind:  wago.CompositeTypeArray,
		Array: wago.FieldTypeDescriptor{Storage: wago.StorageTypeDescriptor{Value: wago.ValueTypeDescriptor{Kind: wago.ValueTypeI32}}},
	}, textI32) {
		t.Fatal("i32 array did not match i32 text")
	}
	if definedTextArrayMatches(wago.DefinedTypeDescriptor{
		Kind:  wago.CompositeTypeArray,
		Array: wago.FieldTypeDescriptor{Storage: wago.StorageTypeDescriptor{Value: wago.ValueTypeDescriptor{Kind: wago.ValueTypeI64}}},
	}, textI32) {
		t.Fatal("i64 array unexpectedly matched i32 text")
	}
}

func TestAllocateTextArrayUsesCallerSelectedStorage(t *testing.T) {
	m := &fakeGCArrayAllocatorModule{storage: wago.GuestGCArrayI16, token: 77}
	token, code := allocateTextArray(m, "AB", textI16, 0)
	if token != 77 || code != ErrOK {
		t.Fatalf("allocateTextArray = token %d code %d", token, code)
	}
	if m.length != 2 || len(m.payload) != 4 {
		t.Fatalf("allocated length=%d bytes=%d, want 2/4", m.length, len(m.payload))
	}
	want := []byte{'A', 0, 'B', 0}
	for i := range want {
		if m.payload[i] != want[i] {
			t.Fatalf("payload[%d]=%#x, want %#x", i, m.payload[i], want[i])
		}
	}
}

func TestAllocatingArgumentErrorsReturnNullWithoutAllocation(t *testing.T) {
	cfg := normalizeConfig(Config{Args: []string{"alpha"}})
	p := &Plugin{cfg: cfg, raw: newInstanceState(cfg)}
	m := &fakeGCArrayAllocatorModule{storage: wago.GuestGCArrayI8}
	host := p.argsReadAllocatedArrayHost(textI8)

	invalidWTF := make([]uint64, 2)
	host(m, []uint64{0, 2}, invalidWTF)
	if invalidWTF[0] != 0 || int32(invalidWTF[1]) != ErrInvalid || m.calls != 0 {
		t.Fatalf("invalid WTF result=%v calls=%d", invalidWTF, m.calls)
	}

	oob := make([]uint64, 2)
	host(m, []uint64{9, 0}, oob)
	if oob[0] != 0 || int32(oob[1]) != ErrRange || m.calls != 0 {
		t.Fatalf("out-of-range result=%v calls=%d", oob, m.calls)
	}
}

func TestAllocatingArgumentSuccessAndAllocationFailure(t *testing.T) {
	cfg := normalizeConfig(Config{Args: []string{"alpha"}})
	p := &Plugin{cfg: cfg, raw: newInstanceState(cfg)}
	host := p.argsReadAllocatedArrayHost(textI8)

	m := &fakeGCArrayAllocatorModule{storage: wago.GuestGCArrayI8, token: 42}
	result := make([]uint64, 2)
	host(m, []uint64{0, 0}, result)
	if result[0] != 42 || int32(result[1]) != ErrOK || string(m.payload) != "alpha" {
		t.Fatalf("successful allocating read=%v payload=%q", result, m.payload)
	}

	failed := &fakeGCArrayAllocatorModule{storage: wago.GuestGCArrayI8, allocErr: errors.New("out of memory")}
	result = make([]uint64, 2)
	host(failed, []uint64{0, 0}, result)
	if result[0] != 0 || int32(result[1]) != ErrNoMemory {
		t.Fatalf("allocation failure=%v, want null/ERR_NO_MEMORY", result)
	}
}

func TestAllocatingTextBindings(t *testing.T) {
	bindings := (&Plugin{}).allocatingTextBindings()
	if len(bindings) != 12 {
		t.Fatalf("allocating text binding count=%d, want 12", len(bindings))
	}
	seen := make(map[string]bool, len(bindings))
	for _, b := range bindings {
		seen[b.name] = true
	}
	for _, name := range []string{
		"args_read_array_i8",
		"env_read_array_i16",
		"fs_preopen_name_read_array_i32",
		"dir_iter_next_array_i8",
	} {
		if !seen[name] {
			t.Fatalf("missing allocating text binding %q", name)
		}
		if _, ok := Imports(Config{})[Module+"."+name]; ok {
			t.Fatalf("raw Imports unexpectedly advertises caller-typed binding %q", name)
		}
	}
}
