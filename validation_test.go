package facet

import "testing"

func TestScalarValidationPrecedesIndexesAndHandles(t *testing.T) {
	imports := Imports(Config{Args: []string{"ok"}, Env: []string{"A=B"}})

	if got := call(t, imports, "args_len_i8", []uint64{99, 2}, 2); int32(got[1]) != ErrInvalid {
		t.Fatalf("args_len_i8 mixed invalid = %v, want ERR_INVALID", got)
	}
	if got := call(t, imports, "env_len_i8", []uint64{99, 99, 2}, 2); int32(got[1]) != ErrInvalid {
		t.Fatalf("env_len_i8 mixed invalid = %v, want ERR_INVALID", got)
	}
	if got := call(t, imports, "fs_preopen_name_len_i8", []uint64{99, 2}, 2); int32(got[1]) != ErrInvalid {
		t.Fatalf("fs_preopen_name_len_i8 mixed invalid = %v, want ERR_INVALID", got)
	}
	if got := call(t, imports, "dir_iter_next_len_i8", []uint64{0, 2}, 5); int32(got[4]) != ErrInvalid {
		t.Fatalf("dir_iter_next_len_i8 mixed invalid = %v, want ERR_INVALID", got)
	}
	if got := call(t, imports, "fd_seek", []uint64{0, 0, 99}, 2); int32(got[1]) != ErrInvalid {
		t.Fatalf("fd_seek mixed invalid = %v, want ERR_INVALID", got)
	}
	if got := call(t, imports, "poll_add_fd", []uint64{0, 0, uint64(PollHangup), 0}, 1); int32(got[0]) != ErrInvalid {
		t.Fatalf("poll_add_fd mixed invalid = %v, want ERR_INVALID", got)
	}
	if got := call(t, imports, "poll_update_fd", []uint64{0, 0, uint64(PollTimer), 0}, 1); int32(got[0]) != ErrInvalid {
		t.Fatalf("poll_update_fd mixed invalid = %v, want ERR_INVALID", got)
	}
}

func TestPollFDHandleValidationPrecedesRegistrationState(t *testing.T) {
	imports := Imports(Config{})
	poll := call(t, imports, "poll_create", nil, 2)
	if poll[0] == 0 || int32(poll[1]) != ErrOK {
		t.Fatalf("poll_create = %v", poll)
	}
	if got := call(t, imports, "poll_update_fd", []uint64{poll[0], 0, uint64(PollReadable), 0}, 1); int32(got[0]) != ErrBadHandle {
		t.Fatalf("poll_update_fd invalid fd = %v, want ERR_BAD_HANDLE", got)
	}
	if got := call(t, imports, "poll_remove_fd", []uint64{poll[0], 0}, 1); int32(got[0]) != ErrBadHandle {
		t.Fatalf("poll_remove_fd invalid fd = %v, want ERR_BAD_HANDLE", got)
	}
}
