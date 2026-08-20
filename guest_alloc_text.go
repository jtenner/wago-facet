package facet

import (
	"errors"
	"fmt"
	"math"

	wago "github.com/wago-org/wago"
)

var errAllocatedTextTypeInvariant = errors.New("facet: allocated text result type invariant")

func allocatingTextWidth(name string) (textWidth, bool) {
	switch name {
	case "args_read_array_i8", "env_read_array_i8", "fs_preopen_name_read_array_i8", "dir_iter_next_array_i8":
		return textI8, true
	case "args_read_array_i16", "env_read_array_i16", "fs_preopen_name_read_array_i16", "dir_iter_next_array_i16":
		return textI16, true
	case "args_read_array_i32", "env_read_array_i32", "fs_preopen_name_read_array_i32", "dir_iter_next_array_i32":
		return textI32, true
	default:
		return 0, false
	}
}

func definedTextArrayMatches(typ wago.DefinedTypeDescriptor, width textWidth) bool {
	if typ.Kind != wago.CompositeTypeArray {
		return false
	}
	storage := typ.Array.Storage
	switch width {
	case textI8:
		return storage.Packed && storage.PackedType == wago.PackedTypeI8
	case textI16:
		return storage.Packed && storage.PackedType == wago.PackedTypeI16
	case textI32:
		return !storage.Packed && storage.Value.Kind == wago.ValueTypeI32
	default:
		return false
	}
}

// validateAllocatingTextImports enforces the Facet rule that the caller selects
// the concrete GC-array result type and that a storage mismatch fails normal
// Wasm instantiation rather than becoming a runtime ERR_TYPE.
func validateAllocatingTextImports(module wago.ModuleView) error {
	for _, imp := range module.Imports() {
		if imp.Kind != wago.ImportFunc || imp.Module != Module {
			continue
		}
		width, relevant := allocatingTextWidth(imp.Name)
		if !relevant {
			continue
		}
		if len(imp.ResultTypes) == 0 {
			return fmt.Errorf("facet: import %s.%s has no exact result type", imp.Module, imp.Name)
		}
		result := imp.ResultTypes[0]
		if result.Kind != wago.ValueTypeReference || !result.Ref.Nullable || !result.Ref.Heap.Defined {
			return fmt.Errorf("facet: import %s.%s must return a nullable caller-defined GC array", imp.Module, imp.Name)
		}
		defined, ok := module.DefinedType(result.Ref.Heap.TypeIndex)
		if !ok {
			return fmt.Errorf("facet: import %s.%s result type %d is unavailable", imp.Module, imp.Name, result.Ref.Heap.TypeIndex)
		}
		if !definedTextArrayMatches(defined, width) {
			return fmt.Errorf("facet: import %s.%s caller result type has the wrong array storage class", imp.Module, imp.Name)
		}
	}
	return nil
}

func allocateTextArray(m wago.HostModule, value string, width textWidth, wtf int32) (uint64, int32) {
	encoded, units, code := encodeText(value, width, wtf)
	if code != ErrOK {
		return 0, code
	}
	if units > math.MaxUint32 {
		return 0, ErrOverflow
	}
	expected, code := textArrayStorage(width)
	if code != ErrOK {
		return 0, code
	}
	allocator, ok := m.(wago.GuestGCArrayAllocatorHostModule)
	if !ok {
		panic(wago.HostTrap{Err: errors.New("facet: Wago GC-array result allocator is unavailable")})
	}
	token, err := allocator.NewGCArrayResult(0, uint32(units), func(payload []byte, info wago.GuestGCArrayInfo) error {
		if info.Storage != expected || info.Length != uint32(units) || len(payload) != len(encoded) {
			return errAllocatedTextTypeInvariant
		}
		copy(payload, encoded)
		return nil
	})
	if err != nil {
		if errors.Is(err, errAllocatedTextTypeInvariant) {
			panic(wago.HostTrap{Err: err})
		}
		// Exact result shape was validated before instantiation. At this point the
		// only guest-visible recoverable failure is inability to allocate the
		// requested fresh array.
		return 0, ErrNoMemory
	}
	if token == 0 {
		panic(wago.HostTrap{Err: errors.New("facet: Wago returned a null token for an allocated GC-array result")})
	}
	return token, ErrOK
}

func (p *Plugin) argsReadAllocatedArrayHost(width textWidth) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 2 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: args_read_array host signature mismatch")})
		}
		wtf := int32(uint32(params[1]))
		if !validWTF(wtf) {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		state := p.stateFor(m)
		state.mu.Lock()
		index := uint32(params[0])
		if uint64(index) >= uint64(len(state.cfg.Args)) {
			state.mu.Unlock()
			results[1] = uint64(uint32(ErrRange))
			return
		}
		value := state.cfg.Args[index]
		state.mu.Unlock()
		results[0], code := allocateTextArray(m, value, width, wtf)
		results[1] = uint64(uint32(code))
	}
}

func (p *Plugin) envReadAllocatedArrayHost(width textWidth) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 3 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: env_read_array host signature mismatch")})
		}
		field := int32(uint32(params[1]))
		if field != EnvName && field != EnvValue {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		wtf := int32(uint32(params[2]))
		if !validWTF(wtf) {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		state := p.stateFor(m)
		state.mu.Lock()
		index := uint32(params[0])
		if uint64(index) >= uint64(len(state.cfg.Env)) {
			state.mu.Unlock()
			results[1] = uint64(uint32(ErrRange))
			return
		}
		value, code := environmentField(state.cfg.Env[index], field)
		state.mu.Unlock()
		if code != ErrOK {
			results[1] = uint64(uint32(code))
			return
		}
		results[0], code = allocateTextArray(m, value, width, wtf)
		results[1] = uint64(uint32(code))
	}
}

func (p *Plugin) preopenNameReadAllocatedArrayHost(width textWidth) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 2 || len(results) < 2 {
			panic(wago.HostTrap{Err: errors.New("facet: fs_preopen_name_read_array host signature mismatch")})
		}
		wtf := int32(uint32(params[1]))
		if !validWTF(wtf) {
			results[1] = uint64(uint32(ErrInvalid))
			return
		}
		state := p.stateFor(m)
		state.mu.Lock()
		index := uint32(params[0])
		if uint64(index) >= uint64(len(state.cfg.Preopens)) {
			state.mu.Unlock()
			results[1] = uint64(uint32(ErrRange))
			return
		}
		value := state.cfg.Preopens[index].Guest
		state.mu.Unlock()
		results[0], code := allocateTextArray(m, value, width, wtf)
		results[1] = uint64(uint32(code))
	}
}

func (p *Plugin) dirIterNextAllocatedArrayHost(width textWidth) wago.HostFunc {
	return func(m wago.HostModule, params, results []uint64) {
		zeroResults(results)
		if len(params) != 2 || len(results) < 5 {
			panic(wago.HostTrap{Err: errors.New("facet: dir_iter_next_array host signature mismatch")})
		}
		wtf := int32(uint32(params[1]))
		if !validWTF(wtf) {
			results[4] = uint64(uint32(ErrInvalid))
			return
		}
		state := p.stateFor(m)
		state.mu.Lock()
		defer state.mu.Unlock()
		iter, code := getIterator(state, uint32(params[0]))
		if code != ErrOK {
			results[4] = uint64(uint32(code))
			return
		}
		snap, code := snapshotDirEntry(iter)
		if code != ErrOK {
			results[4] = uint64(uint32(code))
			return
		}
		if snap == nil {
			results[3] = 1
			results[4] = uint64(uint32(ErrOK))
			return
		}
		token, code := allocateTextArray(m, snap.name, width, wtf)
		if code != ErrOK {
			results[4] = uint64(uint32(code))
			return
		}
		iter.index++
		iter.pending = nil
		results[0] = token
		results[1] = uint64(uint32(snap.kind))
		results[2] = snap.inode
		results[4] = uint64(uint32(ErrOK))
	}
}

func (p *Plugin) allocatingTextBindings() []binding {
	i32 := wago.ValI32
	i64 := wago.ValI64
	anyref := wago.ValAnyRef
	bindings := make([]binding, 0, 12)
	for _, spec := range []struct {
		suffix string
		width  textWidth
	}{
		{"i8", textI8},
		{"i16", textI16},
		{"i32", textI32},
	} {
		bindings = append(bindings,
			binding{"args_read_array_" + spec.suffix, p.argsReadAllocatedArrayHost(spec.width), []wago.ValType{i32, i32}, []wago.ValType{anyref, i32}, CapArgumentsRead, "allocate one argument as the caller-selected concrete GC array type"},
			binding{"env_read_array_" + spec.suffix, p.envReadAllocatedArrayHost(spec.width), []wago.ValType{i32, i32, i32}, []wago.ValType{anyref, i32}, CapEnvironmentRead, "allocate one environment field as the caller-selected concrete GC array type"},
			binding{"fs_preopen_name_read_array_" + spec.suffix, p.preopenNameReadAllocatedArrayHost(spec.width), []wago.ValType{i32, i32}, []wago.ValType{anyref, i32}, CapFilesystemRead, "allocate a preopen display name as the caller-selected concrete GC array type"},
			binding{"dir_iter_next_array_" + spec.suffix, p.dirIterNextAllocatedArrayHost(spec.width), []wago.ValType{i32, i32}, []wago.ValType{anyref, i32, i64, i32, i32}, CapFilesystemRead, "allocate and consume the pending directory entry name as the caller-selected concrete GC array type"},
		)
	}
	return bindings
}
