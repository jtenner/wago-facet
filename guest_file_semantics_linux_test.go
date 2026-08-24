package facet

import (
	"testing"

	wago "github.com/wago-org/wago"
)

func TestPwriteUsesExplicitOffsetWithAppendDescriptor(t *testing.T) {
	memory := make([]byte, 128)
	copy(memory, "append.txt")
	copy(memory[32:], "abc")
	copy(memory[48:], "Z")
	rights := RightPathOpen | RightPathCreate | RightRead | RightWrite | RightSeek | RightTell | RightStat
	p, m, pre := pathTestPlugin(t, memory, rights)

	opened := make([]uint64, 2)
	p.pathOpenMemoryHost(textI8, wago.GuestMemory32)(m, []uint64{
		pre, 0, 0, uint64(len("append.txt")), 0,
		uint64(OpenCreate | OpenTruncate | OpenAppend),
		RightRead | RightWrite | RightSeek | RightTell | RightStat,
	}, opened)
	if opened[0] == 0 || int32(opened[1]) != ErrOK {
		t.Fatalf("path_open append = %v", opened)
	}
	fd := opened[0]

	written := make([]uint64, 2)
	p.fdMemoryIOHost(fdIOWrite, wago.GuestMemory32)(m, []uint64{fd, 0, 32, 3}, written)
	if written[0] != 3 || int32(written[1]) != ErrOK {
		t.Fatalf("initial write = %v", written)
	}
	pwrite := make([]uint64, 2)
	p.fdPositionalMemoryHost(fdIOWrite, wago.GuestMemory32)(m, []uint64{fd, 1, 0, 48, 1}, pwrite)
	if pwrite[0] != 1 || int32(pwrite[1]) != ErrOK {
		t.Fatalf("pwrite = %v", pwrite)
	}

	seek := make([]uint64, 2)
	p.fdSeekHost(m, []uint64{fd, 0, uint64(SeekSet)}, seek)
	if int32(seek[1]) != ErrOK {
		t.Fatalf("seek = %v", seek)
	}
	read := make([]uint64, 2)
	p.fdMemoryIOHost(fdIORead, wago.GuestMemory32)(m, []uint64{fd, 0, 64, 3}, read)
	if read[0] != 3 || int32(read[1]) != ErrOK || string(memory[64:67]) != "aZc" {
		t.Fatalf("readback = %v payload=%q, want aZc", read, memory[64:67])
	}
}

func TestRegularFilePollIsImmediatelyReady(t *testing.T) {
	memory := make([]byte, 64)
	copy(memory, "ready.txt")
	rights := RightPathOpen | RightPathCreate | RightRead | RightWrite
	p, m, pre := pathTestPlugin(t, memory, rights)
	opened := make([]uint64, 2)
	p.pathOpenMemoryHost(textI8, wago.GuestMemory32)(m, []uint64{
		pre, 0, 0, uint64(len("ready.txt")), 0, uint64(OpenCreate), RightRead | RightWrite,
	}, opened)
	if opened[0] == 0 || int32(opened[1]) != ErrOK {
		t.Fatalf("path_open = %v", opened)
	}

	poll := make([]uint64, 2)
	p.pollCreateHost(m, nil, poll)
	if poll[0] == 0 || int32(poll[1]) != ErrOK {
		t.Fatalf("poll_create = %v", poll)
	}
	add := make([]uint64, 1)
	p.pollAddFDHost(m, []uint64{poll[0], opened[0], uint64(PollReadable | PollWritable), 0x55}, add)
	if int32(add[0]) != ErrOK {
		t.Fatalf("poll_add_fd = %v", add)
	}
	wait := make([]uint64, 2)
	p.pollWaitHost(m, []uint64{poll[0], 0}, wait)
	if wait[0] != 1 || int32(wait[1]) != ErrOK {
		t.Fatalf("poll_wait = %v", wait)
	}
	next := make([]uint64, 6)
	p.pollNextHost(m, []uint64{poll[0]}, next)
	if int32(next[0]) != PollSourceFD || next[1] != opened[0] || next[3] != 0x55 || next[4] != 0 || int32(next[5]) != ErrOK {
		t.Fatalf("poll_next = %v", next)
	}
	if uint32(next[2])&(PollReadable|PollWritable) == 0 {
		t.Fatalf("poll events = %#x", next[2])
	}
}
