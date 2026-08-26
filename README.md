# wago-facet

`wago-facet` is the reference [Wago](https://github.com/wago-org/wago) plugin for [Facet](https://github.com/jtenner/facet-spec).

Facet is a small system interface for Core WebAssembly. The import module is `"facet"`.

This repository follows Facet 0.1.

## Status

The planned 0.1.0 release is the experimental reference release for Facet 0.1.

The Runtime plugin path implements the canonical Facet 0.1 import surface. The inventory test requires exactly **261** Facet imports with no duplicate names. Wago's normal registration check verifies scalar ABI categories, and the required instantiation interceptor additionally verifies Facet's structural GC-reference signatures before guest code starts.

Wago callback-scoped guest-storage APIs provide the representation-sensitive operations that Facet needs:

- Memory32 and Memory64 by exact WebAssembly memory index;
- multi-memory guest buffers and iovecs;
- numeric and `v128` Wasm GC arrays;
- nested GC arrays for scatter/gather I/O;
- exact caller-defined GC types;
- caller-typed GC array result allocation;
- zero-copy mutable GC-array payload access.

GC references remain opaque Wago tokens. `wago-facet` does not interpret collector references or object pointers. It resolves them only through Wago's callback-scoped guest-storage APIs.

The preferred integration is `Provider()` through Wago's plugin system. The plugin requires Wago's `instance.instantiate.intercept` authority so a module with a non-canonical structural Facet import is rejected during instantiation. Caller-allocated string and readlink results remain templates: the importing module selects a concrete nullable array type with the required element storage class.

## Implemented surface

The Runtime plugin path includes:

- ABI version, process exit, yield, and opaque resource lifecycle;
- stdin, stdout, and stderr descriptors;
- arguments and environment for UTF-8, UTF-16, and UTF-32;
- strict and WTF text handling;
- system and monotonic clocks;
- cryptographic randomness;
- preopens, descriptor metadata, seek/tell, synchronization, and sizing;
- sequential, positional, and vectored I/O;
- Memory32, Memory64, and GC-array buffer families;
- directory iteration and rewind;
- capability-beneath path operations;
- hard links, symbolic links, and readlink;
- IPv4 and IPv6 socket lifecycle operations;
- stream and datagram I/O;
- DNS resolution;
- level-triggered polling and monotonic timers;
- caller-typed allocating GC string and readlink results.

Facet uses normal Core WebAssembly linking for feature detection. The plugin does not substitute memory 0 for another memory index and does not copy GC arrays through a hidden linear-memory scratch area.

## Conformance

`wago-facet` runs the normative Facet 0.1 suite from a pinned `facet-spec` revision on both Linux/amd64 and Linux/arm64.

The current gate is:

```text
137 / 137 standard WAST tests   PASS
  6 /   6 harness tests         PASS
143 / 143 total tests           PASS
0 failures
0 crashes
0 timeouts
```

CI runs the complete gate with the default collector. It also reruns all 137 standard WAST cases with forced moving-nursery collection on every GC allocation. A differential integration test compares the same argument transfer through Memory32, Memory64, and a Wasm GC array.

Each standard WAST file runs in an isolated subprocess. A runtime crash or timeout is therefore reported as a conformance failure instead of terminating the complete test run.

The six harness tests cover behavior outside standard WAST command composition:

- cross-instance handle isolation;
- process exit status;
- stdout and stderr validation;
- null nested-GC child validation before I/O;
- TCP loopback interaction;
- UDP loopback interaction.

See [`tests/conformance/README.md`](tests/conformance/README.md) for the pinned-suite and local-run instructions.

## Installation

No versioned release has been published yet. Until 0.1.0 is cut, development consumers should pin a reviewed commit instead of relying on the moving `main` branch.

`Provider()` is the preferred Wago plugin integration.

## Supported platforms

The planned 0.1.0 release supports:

- Linux amd64;
- Linux arm64.

The package intentionally uses Linux capability-beneath filesystem operations, polling, and socket primitives. It is not currently a portable Darwin or Windows implementation. Unsupported operating systems are outside the advertised compatibility contract for this release.

## Plugin configuration

A plugin selection can configure streams, environment entries, preopens, and the maximum number of live resource handles.

Example:

```json
{
  "stdin": "inherit",
  "stdout": "inherit",
  "stderr": "inherit",
  "env": ["MODE=development"],
  "preopens": [
    {
      "guest": "~",
      "host": "/srv/my-guest",
      "rights": ["stat", "dir-iterate"]
    }
  ],
  "maxHandles": 1024
}
```

The `~` name has no special ABI behavior. It is an ordinary Facet preopen display name.

If `rights` is omitted, a preopen receives `stat`, `path-open`, and `dir-iterate` rights. An explicit `"rights": []` grants zero Facet rights.

## Guest capabilities

Every import declares a Wago guest capability. Important capability names include:

```text
facet.arguments.read
facet.environment.read
facet.clock.read
facet.random.read
facet.process.exit
facet.stdio.read
facet.stdio.write
facet.fd.manage
facet.fd.read
facet.fd.write
facet.filesystem.read
facet.filesystem.write
facet.filesystem.open
facet.network
facet.poll
```

An embedder can deny these capabilities through normal Wago policy.

## Direct low-level use

`Provider()` is the complete and preferred integration.

Low-level embedders that need only the scalar/resource subset can call `NewInstanceImports(Config)`. The returned `InstanceImports` owns the import map and all backing host descriptors. Call `Close` after the guest instance is finished. There is intentionally no ownership-free `Imports(Config)` helper because such an API cannot safely release guest-created resources or pinned preopen descriptors.

## Development

Run the normal Go checks with:

```sh
go test ./...
go test -race ./...
go vet ./...
```

The ordinary test suite includes seed corpora for text-codec and plugin-configuration fuzz targets. CI also verifies Go 1.22 compatibility.

Run the complete Facet 0.1 conformance gate with the pinned suite as described in [`tests/conformance/README.md`](tests/conformance/README.md).

CI pins both the Wago revision used by the plugin and the Facet specification revision used by conformance testing.

## Runtime integration

See [`docs/wago-runtime-gaps.md`](docs/wago-runtime-gaps.md) for the Wago guest-storage requirements that enabled the complete Facet representation model and the remaining design rules that the plugin relies on.

## License

MIT
