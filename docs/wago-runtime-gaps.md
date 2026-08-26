# Wago runtime integration requirements

Facet makes guest representation choices explicit in import names and structural WebAssembly types.

A conforming implementation must honor those choices exactly. `wago-facet` relies on Wago's callback-scoped guest-storage APIs and pre-instantiation module metadata to do this without raw collector pointers or hidden representation changes.

The runtime gaps that originally blocked complete Facet support are implemented in the Wago revision pinned by this repository. Two runtime-boundary rules remain important for embedders: Facet requires exact structural import validation, and Wago's current `HostModule` does not expose the public invocation `context.Context` to a host callback.

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

## Exact structural import signatures

Wago's declarative host-import registration stores the compact public ABI categories (`[]ValType`). That is sufficient to reject scalar/category mismatches but intentionally collapses structural GC references such as `(ref array)` and a concrete caller-defined array reference into `ValAnyRef`.

Facet therefore requires `instance.instantiate.intercept`. Before an instance starts, `wago-facet` inspects the importing module's exact `ImportSpec.ParamTypes` and `ImportSpec.ResultTypes` through `ModuleView`.

For canonical Facet GC-array input parameters, the exact type must be the non-null, non-exact abstract array reference `(ref array)`. A nullable, exact, defined, or different abstract reference is rejected before guest code runs.

Caller-allocated string and readlink results are the deliberate exception to one fixed concrete result type. Facet defines those imports as templates: the caller selects a nullable concrete array result whose element storage class matches the import suffix. `wago-facet` resolves the caller's defined type with `ModuleView.DefinedType` and rejects a storage mismatch during instantiation.

The canonical inventory test and this structural interceptor are complementary:

- the inventory gate proves the complete 261-name import surface and ordered public ABI categories;
- the instantiation gate proves the structural GC-reference constraints that cannot be represented by `ValType` registration metadata.

## Exact caller types during host calls

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

Because `instance.instantiate.intercept` is required by the plugin, a caller-selected array with the wrong storage class fails instantiation instead of degrading to a runtime `ERR_TYPE`.

## DNS cancellation boundary

Go's resolver accepts a `context.Context`, but Wago's current callback `HostModule` surface does not expose the `context.Context` of the active public guest invocation. `wago-facet` therefore cannot correctly attach a resolver query directly to guest-call cancellation without a future Wago callback-context API.

The plugin still fails closed against indefinite resolver blocking:

1. every DNS lookup has a finite 30-second deadline;
2. plugin shutdown cancels the plugin-owned resolver context and therefore outstanding lookups;
3. Go context cancellation maps to `ERR_CANCELED`;
4. deadline expiry maps to `ERR_TIMED_OUT`.

A future Wago host-call context surface can tighten this so cancellation of the active guest invocation immediately cancels the corresponding DNS lookup. Until then, the finite deadline and lifecycle cancellation are the supported boundary; the plugin does not retain an unbounded `context.Background()` resolver operation.

This work is tracked by [Wago issue #499](https://github.com/wago-org/wago/issues/499) and [`wago-facet` issue #5](https://github.com/jtenner/wago-facet/issues/5).

## GC host-call boundary

A declarative plugin import that transfers a collector object is a real GC boundary even when the module contains no ordinary Wasm GC allocation instruction.

Wago therefore provides:

- a Runtime-owned collector domain for the calling instance;
- exact native frame roots when local Wasm code can be parked;
- an exact empty root set for an import-only module with no local native Wasm frame;
- callback-scoped opaque GC tokens instead of raw `gc.Ref` values;
- result rooting before Wasm resumes.

This keeps the public `HostFuncRef` model strict. A declarative plugin `HostFunc` can still serve different caller-defined GC types because exact structural type and collector-domain state belong to the active import binding and instance, not to one shared function identity.

## Execution serialization

Facet instance state uses one mutex to protect its handle table and mutable resource metadata. Some host operations may block after resolving that state. The current integration relies on Wago's per-instance public-call serialization and callback-scoped caller identity so unrelated public invocations cannot concurrently execute the same Facet instance state.

If Wago later permits independent concurrent guest activations of the same instance, `wago-facet` must split handle-table synchronization from blocking descriptor and network operations before enabling that execution mode. This assumption is an explicit runtime contract, not an accidental data-race defense.

The required synchronization redesign is tracked by [`wago-facet` issue #6](https://github.com/jtenner/wago-facet/issues/6).

## Conformance evidence

The pinned Facet 0.1 suite runs on both supported Linux architectures:

```text
137 / 137 standard WAST tests   PASS
  6 /   6 harness tests         PASS
143 / 143 total tests           PASS
0 failures
0 crashes
0 timeouts
```

The same complete gate runs on Linux/amd64 and Linux/arm64 in CI. See [`../tests/conformance/README.md`](../tests/conformance/README.md) for the executable gate.
