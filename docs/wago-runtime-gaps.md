# Wago runtime hooks needed for complete Facet support

Facet makes guest representation choices explicit in import names.

A conforming implementation must honor those choices exactly.

Current Wago host callbacks expose `HostModule.Memory()`. That method gives the callback-scoped byte slice for memory 0. It does not accept a memory index.

Current Wago host callbacks also carry reference values as opaque slots. They do not expose a public operation that validates and borrows a GC array's logical bytes.

`wago-facet` therefore omits imports that need these operations.

## Indexed linear memory

Facet needs a callback-scoped operation with behavior equivalent to:

```go
MemoryAt(index uint32) (bytes []byte, address64 bool, ok bool)
```

The exact API does not need this shape.

It must provide these properties:

1. The lookup uses the calling module's normal WebAssembly memory index space.
2. It includes imported and locally defined memories.
3. It reports whether the selected memory uses a 32-bit or 64-bit address type.
4. The returned storage is valid only during the host call.
5. A memory grow cannot leave a retained host slice after the callback returns.

This hook would enable the Facet Memory32 and Memory64 families without implicit memory-0 behavior.

## GC array borrowing

Facet also needs a callback-scoped GC array operation.

Conceptually:

```go
WithGCArrayBytes(ref, expectedStorage, accessMode, func(logicalBytes []byte) error) error
```

The runtime must:

1. validate the dynamic array storage class;
2. validate destination mutability for write access;
3. keep the object alive during the callback;
4. preserve Facet's little-endian logical byte view;
5. support byte offsets that begin or end inside an element;
6. end every root, pin, no-move scope, or borrow before the Facet host call returns.

The API does not need to expose the physical GC heap layout.

## Caller-typed GC allocation

Facet's allocating string functions have concrete nullable array result types chosen by the importing guest.

Wago needs an import-registration and host-result mechanism that can:

1. retain the exact concrete result heap type from the import signature;
2. validate the required `i8`, `i16`, or `i32` storage class at link time;
3. allocate the exact caller-selected array type;
4. return the new GC reference as a rooted host result.

A mismatched concrete type must fail WebAssembly instantiation. It must not become a runtime `ERR_TYPE`.

## Why the plugin does not emulate these hooks

Using memory 0 for every `memory_index` would violate Facet multi-memory semantics.

Copying GC storage through a hidden linear-memory scratch area would defeat the representation model that Facet defines.

The plugin therefore fails closed through import absence until Wago exposes the required scoped runtime operations.
