package facet

import wago "github.com/wago-org/wago"

func (p *Plugin) guestStorageBindings() []binding {
	i32 := wago.ValI32
	i64 := wago.ValI64
	anyref := wago.ValAnyRef
	bindings := make([]binding, 0, 57)

	for _, spec := range []struct {
		suffix string
		width  textWidth
	}{
		{"i8", textI8},
		{"i16", textI16},
		{"i32", textI32},
	} {
		bindings = append(bindings,
			binding{"args_read_mem32_" + spec.suffix, p.argsReadMemoryHost(spec.width, wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32, i32}, []wago.ValType{i64, i32}, CapArgumentsRead, "copy one argument into an indexed Memory32 destination"},
			binding{"args_read_mem64_" + spec.suffix, p.argsReadMemoryHost(spec.width, wago.GuestMemory64), []wago.ValType{i32, i32, i32, i64, i64}, []wago.ValType{i64, i32}, CapArgumentsRead, "copy one argument into an indexed Memory64 destination"},
			binding{"args_read_into_array_" + spec.suffix, p.argsReadArrayHost(spec.width), []wago.ValType{i32, i32, anyref, i32, i32}, []wago.ValType{i64, i32}, CapArgumentsRead, "copy one argument into a mutable Wasm GC array destination"},

			binding{"env_read_mem32_" + spec.suffix, p.envReadMemoryHost(spec.width, wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32, i32, i32}, []wago.ValType{i64, i32}, CapEnvironmentRead, "copy one environment field into an indexed Memory32 destination"},
			binding{"env_read_mem64_" + spec.suffix, p.envReadMemoryHost(spec.width, wago.GuestMemory64), []wago.ValType{i32, i32, i32, i32, i64, i64}, []wago.ValType{i64, i32}, CapEnvironmentRead, "copy one environment field into an indexed Memory64 destination"},
			binding{"env_read_into_array_" + spec.suffix, p.envReadArrayHost(spec.width), []wago.ValType{i32, i32, i32, anyref, i32, i32}, []wago.ValType{i64, i32}, CapEnvironmentRead, "copy one environment field into a mutable Wasm GC array destination"},

			binding{"fs_preopen_name_read_mem32_" + spec.suffix, p.preopenNameReadMemoryHost(spec.width, wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32, i32}, []wago.ValType{i64, i32}, CapFilesystemRead, "copy a preopen display name into an indexed Memory32 destination"},
			binding{"fs_preopen_name_read_mem64_" + spec.suffix, p.preopenNameReadMemoryHost(spec.width, wago.GuestMemory64), []wago.ValType{i32, i32, i32, i64, i64}, []wago.ValType{i64, i32}, CapFilesystemRead, "copy a preopen display name into an indexed Memory64 destination"},
			binding{"fs_preopen_name_read_into_array_" + spec.suffix, p.preopenNameReadArrayHost(spec.width), []wago.ValType{i32, i32, anyref, i32, i32}, []wago.ValType{i64, i32}, CapFilesystemRead, "copy a preopen display name into a mutable Wasm GC array destination"},

			binding{"dir_iter_next_mem32_" + spec.suffix, p.dirIterNextReadMemoryHost(spec.width, wago.GuestMemory32), []wago.ValType{i32, i32, i32, i32, i32}, []wago.ValType{i64, i32, i64, i32, i32}, CapFilesystemRead, "copy and consume the pending directory entry name into indexed Memory32"},
			binding{"dir_iter_next_mem64_" + spec.suffix, p.dirIterNextReadMemoryHost(spec.width, wago.GuestMemory64), []wago.ValType{i32, i32, i32, i64, i64}, []wago.ValType{i64, i32, i64, i32, i32}, CapFilesystemRead, "copy and consume the pending directory entry name into indexed Memory64"},
			binding{"dir_iter_next_into_array_" + spec.suffix, p.dirIterNextReadArrayHost(spec.width), []wago.ValType{i32, i32, anyref, i32, i32}, []wago.ValType{i64, i32, i64, i32, i32}, CapFilesystemRead, "copy and consume the pending directory entry name into a mutable Wasm GC array"},
		)
	}

	bindings = append(bindings,
		binding{"random_fill_mem32", p.randomFillMemoryHost(wago.GuestMemory32), []wago.ValType{i32, i32, i32}, []wago.ValType{i64, i32}, CapRandomRead, "fill an indexed Memory32 range with cryptographic randomness"},
		binding{"random_fill_mem64", p.randomFillMemoryHost(wago.GuestMemory64), []wago.ValType{i32, i64, i64}, []wago.ValType{i64, i32}, CapRandomRead, "fill an indexed Memory64 range with cryptographic randomness"},
		binding{"random_fill_array_i8", p.randomFillArrayHost(wago.GuestGCArrayI8), []wago.ValType{anyref, i64, i64}, []wago.ValType{i64, i32}, CapRandomRead, "fill a mutable i8 GC-array byte range with cryptographic randomness"},
		binding{"random_fill_array_i16", p.randomFillArrayHost(wago.GuestGCArrayI16), []wago.ValType{anyref, i64, i64}, []wago.ValType{i64, i32}, CapRandomRead, "fill a mutable i16 GC-array byte range with cryptographic randomness"},
		binding{"random_fill_array_i32", p.randomFillArrayHost(wago.GuestGCArrayI32), []wago.ValType{anyref, i64, i64}, []wago.ValType{i64, i32}, CapRandomRead, "fill a mutable i32 GC-array byte range with cryptographic randomness"},
		binding{"random_fill_array_i64", p.randomFillArrayHost(wago.GuestGCArrayI64), []wago.ValType{anyref, i64, i64}, []wago.ValType{i64, i32}, CapRandomRead, "fill a mutable i64 GC-array byte range with cryptographic randomness"},
		binding{"random_fill_array_v128", p.randomFillArrayHost(wago.GuestGCArrayV128), []wago.ValType{anyref, i64, i64}, []wago.ValType{i64, i32}, CapRandomRead, "fill a mutable v128 GC-array byte range with cryptographic randomness"},
	)
	bindings = append(bindings, p.fdIOBindings()...)
	return bindings
}
