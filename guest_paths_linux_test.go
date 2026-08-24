package facet

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	wago "github.com/wago-org/wago"
)

func TestDecodeGuestTextStrictAndWTF(t *testing.T) {
	if _, code := decodeTextBytes([]byte{0xc0, 0x80}, textI8, 0); code != ErrIllegalSequence {
		t.Fatalf("overlong UTF-8 code = %d", code)
	}
	// WTF-8 for U+DC80 maps reversibly to byte 0x80 on the Linux byte namespace.
	if got, code := decodeTextBytes([]byte{0xed, 0xb2, 0x80}, textI8, 1); code != ErrOK || len(got) != 1 || got[0] != 0x80 {
		t.Fatalf("WTF-8 sentinel = %x, code %d", []byte(got), code)
	}
	var u16 [2]byte
	binary.LittleEndian.PutUint16(u16[:], 0xd800)
	if _, code := decodeTextBytes(u16[:], textI16, 0); code != ErrIllegalSequence {
		t.Fatalf("unpaired UTF-16 code = %d", code)
	}
	var u32 [4]byte
	binary.LittleEndian.PutUint32(u32[:], 0x110000)
	if _, code := decodeTextBytes(u32[:], textI32, 0); code != ErrIllegalSequence {
		t.Fatalf("oversized UTF-32 code = %d", code)
	}
}

func pathTestPlugin(t *testing.T, memory []byte, rights uint64) (*Plugin, *fakeGuestStorageModule, uint64) {
	t.Helper()
	dir := t.TempDir()
	cfg := normalizeConfig(Config{Preopens: []Preopen{{Guest: "~", Host: dir, Rights: rights}}})
	p := &Plugin{cfg: cfg, raw: newInstanceState(cfg)}
	storage := &fakeGuestStorage{
		memoryInfo: wago.GuestMemoryInfo{AddressType: wago.GuestMemory32},
		memory:     memory,
	}
	m := &fakeGuestStorageModule{storage: storage}
	pre := make([]uint64, 2)
	p.preopenGetHost(m, []uint64{0}, pre)
	if pre[0] == 0 || int32(pre[1]) != ErrOK {
		t.Fatalf("fs_preopen_get = %v", pre)
	}
	t.Cleanup(p.raw.closeAll)
	return p, m, pre[0]
}

func TestPathOpenRegularFileRoundTrip(t *testing.T) {
	memory := make([]byte, 128)
	copy(memory, "hello.txt")
	copy(memory[32:], "facet")
	rights := RightPathOpen | RightPathCreate | RightPathRemove | RightPathRename | RightStat |
		RightRead | RightWrite | RightSeek | RightTell | RightSetSize | RightSync
	p, m, pre := pathTestPlugin(t, memory, rights)

	open := make([]uint64, 2)
	p.pathOpenMemoryHost(textI8, wago.GuestMemory32)(m, []uint64{
		pre, 0, 0, uint64(len("hello.txt")), 0, uint64(OpenCreate | OpenTruncate),
		RightRead | RightWrite | RightSeek | RightTell | RightStat | RightSetSize | RightSync,
	}, open)
	if open[0] == 0 || int32(open[1]) != ErrOK {
		t.Fatalf("path_open = %v", open)
	}
	fd := open[0]

	write := make([]uint64, 2)
	p.fdMemoryIOHost(fdIOWrite, wago.GuestMemory32)(m, []uint64{fd, 0, 32, 5}, write)
	if write[0] != 5 || int32(write[1]) != ErrOK {
		t.Fatalf("fd_write = %v", write)
	}
	seek := make([]uint64, 2)
	p.fdSeekHost(m, []uint64{fd, 0, uint64(SeekSet)}, seek)
	if seek[0] != 0 || int32(seek[1]) != ErrOK {
		t.Fatalf("fd_seek = %v", seek)
	}
	for i := 64; i < 69; i++ {
		memory[i] = 0
	}
	read := make([]uint64, 2)
	p.fdMemoryIOHost(fdIORead, wago.GuestMemory32)(m, []uint64{fd, 0, 64, 5}, read)
	if read[0] != 5 || int32(read[1]) != ErrOK || string(memory[64:69]) != "facet" {
		t.Fatalf("fd_read = %v payload=%q", read, memory[64:69])
	}
}

func TestPathOpenRejectsCapabilityEscapes(t *testing.T) {
	memory := make([]byte, 128)
	copy(memory, "../outside.txt")
	rights := RightPathOpen | RightRead
	p, m, pre := pathTestPlugin(t, memory, rights)
	open := make([]uint64, 2)
	p.pathOpenMemoryHost(textI8, wago.GuestMemory32)(m, []uint64{pre, 0, 0, 14, 0, 0, RightRead}, open)
	if open[0] != 0 || int32(open[1]) != ErrPermission {
		t.Fatalf("parent escape = %v, want ERR_PERMISSION", open)
	}
}

func TestPathOpenRejectsAbsoluteSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	memory := make([]byte, 64)
	copy(memory, "escape/secret")
	cfg := normalizeConfig(Config{Preopens: []Preopen{{Guest: "~", Host: root, Rights: RightPathOpen | RightRead}}})
	p := &Plugin{cfg: cfg, raw: newInstanceState(cfg)}
	storage := &fakeGuestStorage{memoryInfo: wago.GuestMemoryInfo{AddressType: wago.GuestMemory32}, memory: memory}
	m := &fakeGuestStorageModule{storage: storage}
	defer p.raw.closeAll()
	pre := make([]uint64, 2)
	p.preopenGetHost(m, []uint64{0}, pre)
	open := make([]uint64, 2)
	p.pathOpenMemoryHost(textI8, wago.GuestMemory32)(m, []uint64{pre[0], 0, 0, 13, 0, 0, RightRead}, open)
	if open[0] != 0 || int32(open[1]) != ErrPermission {
		t.Fatalf("symlink escape = %v, want ERR_PERMISSION", open)
	}
}
