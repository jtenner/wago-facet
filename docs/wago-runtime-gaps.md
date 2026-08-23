# Wago runtime integration requirements

Facet makes guest representation choices explicit in import names.

A conforming implementation must honor those choices exactly. `wago-facet` relies on Wago's callback-scoped guest-storage APIs to do this without raw collector pointers or hidden representation changes.

The runtime gaps that originally blocked complete Facet support are now implemented in the Wago revision pinned by this repository.

## Indexed linear memory

Facet Memory32 and Memory64 imports use Wago `GuestStorage` access by exact WebAssembly memory index.

The integration requires these properties:

1. The lookup uses the calling module's normal WebAssembly memory index space.
2. Imported and locally defined memories use the same index space.
3. `MemoryInfo` reports whether the selected memory is Memory32 or Memory64.
4. `MemoryRange` validates unsigned offset and length arithmetic before it returns storage.
5. A returned slice is callback-scoped and aliases the selected guest memory directly.
6. Guest re-entry or memory growth cannot invalidate a live borrowed slice.

`wago-facet` does not substitute memory 0 for another memory index.

## Wasm GC array access

GC-reference host slots are opaque Wago tokens. The plugin must not interpret them as collector references or object pointers.

Inside `WithGuestStorage`, the plugin uses:

```go
ref, err := storage.GCRef(slot)
info, err := storage.GCArrayInfo(ref)
payload, info, err := storage.GCArrayBytes(ref, access)
```

The integration requires Wago to:

1. validate token ownership and callback lifetime;
2. validate the dynamic array storage class;
3. validate mutability for write access;
4. keep the object stable for the complete borrow;
5. expose only the logical numeric or `v128` payload;
6. reject raw byte access to reference arrays;
7. permit nested reference-array traversal through `GCArrayRef`;
8. expire every borrowed slice and `GuestGCRef` when the borrow ends.

Mutable numeric arrays can be used as zero-copy syscall buffers. No raw pointer API is required.

## Exact caller types

Facet has imports whose concrete GC array type is selected by the importing module.

The plugin uses the exact active import metadata through:

```go
storage.ImportParamType(index)
storage.ImportResultType(index)
storage.DefinedType(typeIndex)
```

The defined-type indexes remain local to the calling module. The plugin does not attach one global exact GC type to a generic host callback.

## Caller-typed GC allocation

Allocating Facet string and readlink imports use `GuestGCArrayAllocatorHostModule.NewGCArrayResult`.

Wago must:

1. read the exact result heap type from the active caller signature;
2. allocate that concrete array type in the caller's GC domain;
3. keep the new object rooted while the initializer runs;
4. allow initialization before immutable publication;
5. return an ephemeral host-result token;
6. validate that token against the exact result type before Wasm resumes.

`wago-facet` also uses `instance.instantiate.intercept` when granted so a caller-selected array with the wrong storage class fails instantiation instead of degrading to a runtime `ERR_TYPE`.

## GC host-call boundary

A declarative plugin import that transfers a collector object is a real GC boundary even when the module contains no ordinary Wasm GC allocation instruction.

Wago therefore provides:

- a Runtime-owned collector domain for the calling instance;
- exact native frame roots when local Wasm code can be parked;
- an exact empty root set for an import-only module with no local native Wasm frame;
- callback-scoped opaque GC tokens instead of raw `gc.Ref` values;
- result rooting before Wasm resumes.

This keeps the public `HostFuncRef` model strict. A declarative plugin `HostFunc` can still serve different caller-defined GC types because exact structural type and collector-domain state belong to the active import binding and instance, not to one shared function identity.

## Conformance evidence

The pinned Facet 0.1 suite now passes through this integration:

```text
137 / 137 standard WAST tests   PASS
  6 /   6 harness tests         PASS
143 / 143 total tests           PASS
0 failures
0 crashes
0 timeouts
```

See [`../tests/conformance/README.md`](../tests/conformance/README.md) for the executable gate.
