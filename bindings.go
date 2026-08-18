package facet

import wago "github.com/wago-org/wago"

func (p *Plugin) bindings() []binding {
	i32 := wago.ValI32
	i64 := wago.ValI64
	return []binding{
		{"abi_version", p.abiVersionHost, nil, []wago.ValType{i32}, CapCore, "report Facet ABI generation 1"},
		{"handle_close", p.handleCloseHost, []wago.ValType{i32}, []wago.ValType{i32}, CapFDManage, "close an opaque Facet resource handle"},
		{"proc_exit", p.procExitHost, []wago.ValType{i32}, nil, CapProcessExit, "terminate the current guest invocation"},
		{"proc_yield", p.procYieldHost, nil, []wago.ValType{i32}, CapSchedulerYield, "yield execution cooperatively"},
		{"stdio_stdin", p.stdioStdinHost, nil, []wago.ValType{i32, i32}, CapStdinRead, "obtain the standard-input descriptor"},
		{"stdio_stdout", p.stdioStdoutHost, nil, []wago.ValType{i32, i32}, CapStdoutWrite, "obtain the standard-output descriptor"},
		{"stdio_stderr", p.stdioStderrHost, nil, []wago.ValType{i32, i32}, CapStdoutWrite, "obtain the standard-error descriptor"},

		{"args_count", p.argsCountHost, nil, []wago.ValType{i32, i32}, CapArgumentsRead, "report the argument count"},
		{"args_len_i8", p.argsLenI8Host, []wago.ValType{i32, i32}, []wago.ValType{i64, i32}, CapArgumentsRead, "report an argument length in i8 code units"},
		{"args_len_i16", p.argsLenI16Host, []wago.ValType{i32, i32}, []wago.ValType{i64, i32}, CapArgumentsRead, "report an argument length in i16 code units"},
		{"args_len_i32", p.argsLenI32Host, []wago.ValType{i32, i32}, []wago.ValType{i64, i32}, CapArgumentsRead, "report an argument length in i32 code points"},
		{"env_count", p.envCountHost, nil, []wago.ValType{i32, i32}, CapEnvironmentRead, "report the environment-entry count"},
		{"env_len_i8", p.envLenI8Host, []wago.ValType{i32, i32, i32}, []wago.ValType{i64, i32}, CapEnvironmentRead, "report an environment field length in i8 code units"},
		{"env_len_i16", p.envLenI16Host, []wago.ValType{i32, i32, i32}, []wago.ValType{i64, i32}, CapEnvironmentRead, "report an environment field length in i16 code units"},
		{"env_len_i32", p.envLenI32Host, []wago.ValType{i32, i32, i32}, []wago.ValType{i64, i32}, CapEnvironmentRead, "report an environment field length in i32 code points"},

		{"clock_system_now", p.clockSystemNowHost, nil, []wago.ValType{i64, i32, i32}, CapClockRead, "read the system clock"},
		{"clock_monotonic_now", p.clockMonotonicNowHost, nil, []wago.ValType{i64, i32}, CapClockRead, "read the monotonic clock"},
		{"clock_monotonic_resolution", p.clockMonotonicResolutionHost, nil, []wago.ValType{i64, i32}, CapClockRead, "read the monotonic clock resolution"},
		{"sleep_for", p.sleepForHost, []wago.ValType{i64}, []wago.ValType{i32}, CapClockRead, "sleep for a monotonic duration"},
		{"sleep_until", p.sleepUntilHost, []wago.ValType{i64}, []wago.ValType{i32}, CapClockRead, "sleep until an absolute monotonic deadline"},
		{"random_u64", p.randomU64Host, nil, []wago.ValType{i64, i32}, CapRandomRead, "return a cryptographically random u64"},

		{"fs_preopen_count", p.preopenCountHost, nil, []wago.ValType{i32, i32}, CapFilesystemRead, "report the configured preopen count"},
		{"fs_preopen_get", p.preopenGetHost, []wago.ValType{i32}, []wago.ValType{i32, i32}, CapFilesystemRead, "obtain a configured directory preopen"},
		{"fs_preopen_name_len_i8", p.preopenNameLenI8Host, []wago.ValType{i32, i32}, []wago.ValType{i64, i32}, CapFilesystemRead, "report a preopen display-name length in i8 code units"},
		{"fs_preopen_name_len_i16", p.preopenNameLenI16Host, []wago.ValType{i32, i32}, []wago.ValType{i64, i32}, CapFilesystemRead, "report a preopen display-name length in i16 code units"},
		{"fs_preopen_name_len_i32", p.preopenNameLenI32Host, []wago.ValType{i32, i32}, []wago.ValType{i64, i32}, CapFilesystemRead, "report a preopen display-name length in i32 code points"},

		{"fd_rights", p.fdRightsHost, []wago.ValType{i32}, []wago.ValType{i64, i32}, CapFDManage, "report descriptor rights"},
		{"fd_get_flags", p.fdGetFlagsHost, []wago.ValType{i32}, []wago.ValType{i32, i32}, CapFDManage, "report descriptor flags"},
		{"fd_set_flags", p.fdSetFlagsHost, []wago.ValType{i32, i32}, []wago.ValType{i32}, CapFDManage, "set portable descriptor flags"},
		{"fd_stat", p.fdStatHost, []wago.ValType{i32}, []wago.ValType{i32, i32, i64, i64, i32, i64, i32, i64, i32, i32}, CapFDManage, "report portable descriptor metadata"},
		{"fd_seek", p.fdSeekHost, []wago.ValType{i32, i64, i32}, []wago.ValType{i64, i32}, CapFDManage, "seek a seekable descriptor"},
		{"fd_tell", p.fdTellHost, []wago.ValType{i32}, []wago.ValType{i64, i32}, CapFDManage, "report a seekable descriptor position"},
		{"fd_set_size", p.fdSetSizeHost, []wago.ValType{i32, i64}, []wago.ValType{i32}, CapFilesystemWrite, "set a regular-file size"},
		{"fd_sync", p.fdSyncHost, []wago.ValType{i32}, []wago.ValType{i32}, CapFilesystemWrite, "synchronize descriptor data and metadata"},
		{"fd_datasync", p.fdDatasyncHost, []wago.ValType{i32}, []wago.ValType{i32}, CapFilesystemWrite, "synchronize descriptor data"},

		{"dir_iter_open", p.dirIterOpenHost, []wago.ValType{i32}, []wago.ValType{i32, i32}, CapFilesystemRead, "open a directory iterator"},
		{"dir_iter_next_len_i8", p.dirIterNextLenI8Host, []wago.ValType{i32, i32}, []wago.ValType{i64, i32, i64, i32, i32}, CapFilesystemRead, "snapshot the next directory entry and report its i8 name length"},
		{"dir_iter_next_len_i16", p.dirIterNextLenI16Host, []wago.ValType{i32, i32}, []wago.ValType{i64, i32, i64, i32, i32}, CapFilesystemRead, "snapshot the next directory entry and report its i16 name length"},
		{"dir_iter_next_len_i32", p.dirIterNextLenI32Host, []wago.ValType{i32, i32}, []wago.ValType{i64, i32, i64, i32, i32}, CapFilesystemRead, "snapshot the next directory entry and report its i32 name length"},
		{"dir_iter_rewind", p.dirIterRewindHost, []wago.ValType{i32}, []wago.ValType{i32}, CapFilesystemRead, "rewind a directory iterator and discard its pending entry"},

		{"socket_open", p.socketOpenHost, []wago.ValType{i32, i32, i32, i32}, []wago.ValType{i32, i32}, CapNetwork, "open an IPv4 or IPv6 TCP/UDP socket"},
		{"socket_bind", p.socketBindHost, []wago.ValType{i32, i32, i64, i64, i32, i32}, []wago.ValType{i32}, CapNetwork, "bind a socket to a local address"},
		{"socket_connect", p.socketConnectHost, []wago.ValType{i32, i32, i64, i64, i32, i32}, []wago.ValType{i32}, CapNetwork, "connect a socket to a peer"},
		{"socket_listen", p.socketListenHost, []wago.ValType{i32, i32}, []wago.ValType{i32}, CapNetwork, "listen on a bound stream socket"},
		{"socket_accept", p.socketAcceptHost, []wago.ValType{i32, i32}, []wago.ValType{i32, i32, i64, i64, i32, i32, i32}, CapNetwork, "accept one stream connection"},
		{"socket_local_address", p.socketLocalAddressHost, []wago.ValType{i32}, []wago.ValType{i32, i64, i64, i32, i32, i32}, CapNetwork, "report a socket local address"},
		{"socket_peer_address", p.socketPeerAddressHost, []wago.ValType{i32}, []wago.ValType{i32, i64, i64, i32, i32, i32}, CapNetwork, "report a connected socket peer address"},
		{"socket_shutdown", p.socketShutdownHost, []wago.ValType{i32, i32}, []wago.ValType{i32}, CapNetwork, "shut down a connected stream socket"},

		{"poll_create", p.pollCreateHost, nil, []wago.ValType{i32, i32}, CapPoll, "create a Facet readiness set"},
		{"poll_add_fd", p.pollAddFDHost, []wago.ValType{i32, i32, i32, i64}, []wago.ValType{i32}, CapPoll, "add descriptor readiness interest"},
		{"poll_update_fd", p.pollUpdateFDHost, []wago.ValType{i32, i32, i32, i64}, []wago.ValType{i32}, CapPoll, "update descriptor readiness interest"},
		{"poll_remove_fd", p.pollRemoveFDHost, []wago.ValType{i32, i32}, []wago.ValType{i32}, CapPoll, "remove descriptor readiness interest"},
		{"poll_add_timer", p.pollAddTimerHost, []wago.ValType{i32, i64, i64}, []wago.ValType{i32, i32}, CapPoll, "add a one-shot monotonic timer"},
		{"poll_remove_timer", p.pollRemoveTimerHost, []wago.ValType{i32, i32}, []wago.ValType{i32}, CapPoll, "remove a one-shot timer"},
		{"poll_wait", p.pollWaitHost, []wago.ValType{i32, i64}, []wago.ValType{i32, i32}, CapPoll, "wait for a level-triggered readiness snapshot"},
		{"poll_next", p.pollNextHost, []wago.ValType{i32}, []wago.ValType{i32, i32, i32, i64, i32, i32}, CapPoll, "drain one readiness snapshot record"},
	}
}
