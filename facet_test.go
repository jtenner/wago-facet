package facet

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	wago "github.com/wago-org/wago"
)

type testHostModule struct{ memory []byte }

func (m testHostModule) Memory() []byte { return m.memory }

func hostFunc(t *testing.T, imports wago.Imports, name string) wago.HostFunc {
	t.Helper()
	value, ok := imports[Module+"."+name]
	if !ok {
		t.Fatalf("missing import %s.%s", Module, name)
	}
	fn, ok := value.(wago.HostFunc)
	if !ok {
		t.Fatalf("import %s.%s has type %T, want wago.HostFunc", Module, name, value)
	}
	return fn
}

func call(t *testing.T, imports wago.Imports, name string, params []uint64, results int) []uint64 {
	t.Helper()
	out := make([]uint64, results)
	hostFunc(t, imports, name)(testHostModule{}, params, out)
	return out
}

func TestDefinition(t *testing.T) {
	def := Definition()
	if def.ID != ID || def.Name != "Facet" || def.Version != Version {
		t.Fatalf("definition = %#v", def)
	}
	if len(def.Authorities) != 4 {
		t.Fatalf("authority count = %d, want 4", len(def.Authorities))
	}
	found := false
	for _, req := range def.Authorities {
		if req.Name == wago.AuthorityHostImportDefine {
			found = len(req.Scope.Modules) == 1 && req.Scope.Modules[0] == Module
		}
	}
	if !found {
		t.Fatal("host.import.define is not scoped exactly to facet")
	}
	if Provider().New == nil {
		t.Fatal("Provider.New is nil")
	}
}

func TestRawCoreAndTextMetadata(t *testing.T) {
	imports := Imports(Config{Args: []string{"alpha", "caf\u00e9"}, Env: []string{"KEY=value"}})
	if got := call(t, imports, "abi_version", nil, 1)[0]; got != 1 {
		t.Fatalf("abi_version = %d", got)
	}
	got := call(t, imports, "args_count", nil, 2)
	if got[0] != 2 || got[1] != uint64(ErrOK) {
		t.Fatalf("args_count = %v", got)
	}
	got = call(t, imports, "args_len_i16", []uint64{1, 0}, 2)
	if got[0] != 4 || got[1] != uint64(ErrOK) {
		t.Fatalf("args_len_i16 = %v", got)
	}
	got = call(t, imports, "env_len_i8", []uint64{0, uint64(EnvValue), 0}, 2)
	if got[0] != 5 || got[1] != uint64(ErrOK) {
		t.Fatalf("env_len_i8 = %v", got)
	}
	got = call(t, imports, "env_len_i8", []uint64{0, 99, 0}, 2)
	if got[0] != 0 || int32(got[1]) != ErrInvalid {
		t.Fatalf("invalid env field = %v", got)
	}
}

func TestHandleZeroAndStaleHandles(t *testing.T) {
	imports := Imports(Config{})
	if got := call(t, imports, "handle_close", []uint64{0}, 1)[0]; int32(got) != ErrBadHandle {
		t.Fatalf("close(0) = %d", got)
	}
	first := call(t, imports, "stdio_stdout", nil, 2)
	if first[0] == 0 || first[1] != uint64(ErrOK) {
		t.Fatalf("stdout = %v", first)
	}
	if got := call(t, imports, "handle_close", []uint64{first[0]}, 1)[0]; int32(got) != ErrOK {
		t.Fatalf("close stdout = %d", got)
	}
	second := call(t, imports, "stdio_stdout", nil, 2)
	if second[0] == 0 || second[0] == first[0] {
		t.Fatalf("reopened stdout reused handle: first=%d second=%d", first[0], second[0])
	}
	if got := call(t, imports, "handle_close", []uint64{first[0]}, 1)[0]; int32(got) != ErrBadHandle {
		t.Fatalf("stale close = %d", got)
	}
}

func TestPreopenAndDirectoryIteratorMetadata(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	imports := Imports(Config{Preopens: []Preopen{{Guest: "~", Host: dir, Rights: RightStat | RightDirIterate}}})
	got := call(t, imports, "fs_preopen_count", nil, 2)
	if got[0] != 1 || got[1] != uint64(ErrOK) {
		t.Fatalf("preopen count = %v", got)
	}
	name := call(t, imports, "fs_preopen_name_len_i8", []uint64{0, 0}, 2)
	if name[0] != 1 || name[1] != uint64(ErrOK) {
		t.Fatalf("preopen name len = %v", name)
	}
	pre := call(t, imports, "fs_preopen_get", []uint64{0}, 2)
	iter := call(t, imports, "dir_iter_open", []uint64{pre[0]}, 2)
	if iter[0] == 0 || iter[1] != uint64(ErrOK) {
		t.Fatalf("dir_iter_open = %v", iter)
	}
	entry := call(t, imports, "dir_iter_next_len_i8", []uint64{iter[0], 0}, 5)
	if entry[0] != uint64(len("hello.txt")) || int32(entry[1]) != FileTypeRegular || entry[3] != 0 || entry[4] != uint64(ErrOK) {
		t.Fatalf("dir entry = %v", entry)
	}
	again := call(t, imports, "dir_iter_next_len_i8", []uint64{iter[0], 0}, 5)
	if again[0] != entry[0] || again[2] != entry[2] {
		t.Fatalf("pending snapshot changed: first=%v second=%v", entry, again)
	}
}

func TestImmediateTimerPollSnapshot(t *testing.T) {
	imports := Imports(Config{})
	poll := call(t, imports, "poll_create", nil, 2)
	timer := call(t, imports, "poll_add_timer", []uint64{poll[0], 0, 0x1234}, 2)
	if timer[0] == 0 || timer[1] != uint64(ErrOK) {
		t.Fatalf("poll_add_timer = %v", timer)
	}
	wait := call(t, imports, "poll_wait", []uint64{poll[0], math.MaxUint64}, 2)
	if wait[0] != 1 || wait[1] != uint64(ErrOK) {
		t.Fatalf("poll_wait = %v", wait)
	}
	event := call(t, imports, "poll_next", []uint64{poll[0]}, 6)
	if int32(event[0]) != PollSourceTimer || uint32(event[1]) != uint32(timer[0]) || uint32(event[2]) != PollTimer || event[3] != 0x1234 || event[4] != 0 || event[5] != uint64(ErrOK) {
		t.Fatalf("poll event = %v", event)
	}
	done := call(t, imports, "poll_next", []uint64{poll[0]}, 6)
	if done[4] != 1 || done[5] != uint64(ErrOK) {
		t.Fatalf("poll done = %v", done)
	}
}

func TestRepresentationDependentImportsAreAbsent(t *testing.T) {
	imports := Imports(Config{})
	for _, name := range []string{"fd_read_mem32", "fd_write_mem64", "random_fill_array_i8", "args_read_mem32_i8", "path_open_mem32_i8"} {
		if _, ok := imports[Module+"."+name]; ok {
			t.Fatalf("%s unexpectedly registered without exact Wago guest-storage support", name)
		}
	}
}

func TestPluginConfigValidation(t *testing.T) {
	good := json.RawMessage(`{"preopens":[{"guest":"~","host":"/tmp","rights":["stat","dir-iterate"]}],"maxHandles":64}`)
	if err := validatePluginConfig(good); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	bad := json.RawMessage(`{"preopens":[{"guest":"x","host":"/tmp","rights":["magic"]}]}`)
	if err := validatePluginConfig(bad); err == nil {
		t.Fatal("unknown right was accepted")
	}
}
